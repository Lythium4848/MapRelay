package server

import (
	"MapRelay/logging"
	"bufio"
	"encoding/base64"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var logger = logging.Named("Server")
var upgrader = websocket.Upgrader{}

type wsMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func RunServer(args []string) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	port := fs.String("port", "8000", "Port to listen on")
	configPath := fs.String("config", "server_config.json", "Path to server config JSON")
	presetsPath := fs.String("presets", "presets.json", "Path to presets JSON store")
	if err := fs.Parse(args); err != nil {
		logger.Fatal("Failed to parse server flags", zap.Error(err))
		return
	}

	c, err := LoadConfig(*configPath)
	if err != nil {
		logger.Fatal("Failed to load config", zap.Error(err))
		return
	}
	config = c
	if err := initPresetStore(*presetsPath); err != nil {
		logger.Fatal("Failed to init preset store", zap.Error(err))
		return
	}

	http.HandleFunc("/", handleSocket)
	http.HandleFunc("/api/presets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleListPresets(w, r)
			return
		}
		handleCreateOrUpdatePreset(w, r)
	})

	logger.Info("MapRelay server listen on port " + *port)
	err = http.ListenAndServe(":"+*port, nil)
	if err != nil {
		logger.Fatal("Failed to start server", zap.Error(err))
		return
	}
}

type compileRequest struct {
	VMF      string `json:"vmf"`
	VMFName  string `json:"vmfName,omitempty"`
	VMFData  []byte `json:"vmfData,omitempty"`
	Preset   string `json:"preset"`
	Password string `json:"password"`
}

func handleSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("Failed to upgrade connection", zap.Error(err))
		return
	}

	defer conn.Close()

	_, msg, _ := conn.ReadMessage()
	logger.Info("Received message", zap.String("message", string(msg)))

	var req compileRequest
	_ = json.Unmarshal(msg, &req)

	if err := CheckPassword(req.Password); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("AUTH_FAILED"))
		return
	}

	var p Preset
	found := false

	for _, pr := range getAllPresets() {
		if pr.Name == req.Preset {
			p = pr
			found = true
			break
		}
	}

	if !found {
		conn.WriteMessage(websocket.TextMessage, []byte("ERROR: preset not found"))
		return
	}

	// Mutex to serialize writes to the websocket from multiple goroutines
	var writeMu sync.Mutex
	sendJSON := func(t, m string) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.WriteJSON(wsMessage{Type: t, Message: m})
	}

	// Determine VMF path: if data is provided, save to a temp location on the server
	vmfPath := req.VMF
	tmpDir := ""
	if len(req.VMFData) > 0 {
		name := req.VMFName
		if name == "" {
			name = "uploaded.vmf"
		}
		// ensure base name only
		name = filepath.Base(name)
		d, err := os.MkdirTemp("", "maprelay-*")
		if err != nil {
			sendJSON("error", "failed to create temp dir: "+err.Error())
			return
		}
		tmpDir = d
		vmfPath = filepath.Join(tmpDir, name)
		if err := os.WriteFile(vmfPath, req.VMFData, 0644); err != nil {
			sendJSON("error", "failed to write uploaded vmf: "+err.Error())
			return
		}
		sendJSON("info", "Received VMF upload: "+vmfPath)
		defer os.RemoveAll(tmpDir)
	}

	vars := buildVarMap(vmfPath)

	sendJSON("info", "Starting compile...")
	for _, step := range p.Steps {
		progPath := config.Programs[step.Program]
		if progPath == "" {
			sendJSON("error", "program not configured: "+step.Program)
			return
		}

		resolvedPath := resolveProgramPath(progPath)
		expanded := expandArgs(step.Args, vars)

		// Build command and args, with Wine wrapping on Linux for .exe
		var cmdName string
		var cmdArgs []string
		useWine := runtime.GOOS == "linux" && strings.HasSuffix(strings.ToLower(resolvedPath), ".exe")
		if useWine {
			wine := config.WinePath
			if wine == "" {
				wine = "wine"
			}
			// Convert Unix absolute paths in arguments to Wine (Z:\\) paths so Windows tools
			// don't treat them as relative and prepend the working directory.
			for i, a := range expanded {
				if len(a) > 0 && strings.HasPrefix(a, "/") {
					// Map /foo/bar -> Z:\\foo\\bar
					repl := strings.ReplaceAll(a, "/", "\\")
					if strings.HasPrefix(repl, "\\") {
						repl = repl[1:]
					}
					expanded[i] = "Z:\\" + repl
				}
			}
			cmdName = wine
			cmdArgs = append([]string{resolvedPath}, expanded...)
		} else {
			cmdName = resolvedPath
			cmdArgs = expanded
		}

		sendJSON("info", "Running "+cmdName+" with args: "+joinArgs(cmdArgs))

		cmd := exec.Command(cmdName, cmdArgs...)
		// Set working directory:
		// - For Windows tools (.exe), set to the executable directory so dependent DLLs (e.g., filesystem_stdio.dll)
		//   are found alongside the tool. This applies when running under Wine on Linux or natively on Windows.
		// - For non-Windows binaries, keep the VMF directory so any relative paths in args resolve there.
		workDir := vars["$path"]
		if strings.HasSuffix(strings.ToLower(resolvedPath), ".exe") {
			workDir = filepath.Dir(resolvedPath)
		}
		cmd.Dir = workDir
		if useWine {
			cmd.Env = append(os.Environ(), "WINEDEBUG=-all")
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			sendJSON("error", "cannot get stdout: "+err.Error())
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			sendJSON("error", "cannot get stderr: "+err.Error())
			return
		}

		if err := cmd.Start(); err != nil {
			sendJSON("error", "failed to start: "+err.Error())
			return
		}

		// Stream output
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			scan := bufio.NewScanner(stdout)
			for scan.Scan() {
				sendJSON(step.Program, scan.Text())
			}
		}()
		go func() {
			defer wg.Done()
			scan := bufio.NewScanner(stderr)
			for scan.Scan() {
				sendJSON(step.Program, scan.Text())
			}
		}()

		wg.Wait()

		if err := cmd.Wait(); err != nil {
			sendJSON("error", "process exited with error: "+err.Error())
			return
		}

		sendJSON("step_done", step.Program)
	}

	// After successful compile, send the compiled BSP back to the client
	bspPath := vars["$bsp"]
	if bspPath != "" {
		if b, err := os.ReadFile(bspPath); err == nil {
			// send typed message with base64 payload
			type bspMsg struct {
				Type string `json:"type"`
				Name string `json:"name"`
				Data string `json:"data"`
			}
			encoded := base64.StdEncoding.EncodeToString(b)
			writeMu.Lock()
			_ = conn.WriteJSON(bspMsg{Type: "bsp", Name: filepath.Base(bspPath), Data: encoded})
			writeMu.Unlock()
		} else {
			sendJSON("error", "failed to read bsp: "+err.Error())
			return
		}
	}

	sendJSON("done", "")
}

func joinArgs(a []string) string {
	if len(a) == 0 {
		return ""
	}
	b, _ := json.Marshal(a)
	return string(b)
}

func buildVarMap(vmf string) map[string]string {
	vars := map[string]string{}
	abs := vmf
	if a, err := filepath.Abs(vmf); err == nil {
		abs = a
	}
	dir := filepath.Dir(abs)

	dir = strings.TrimRight(dir, string(os.PathSeparator))
	base := filepath.Base(abs)
	name := base
	if dot := strings.LastIndex(base, "."); dot > 0 {
		name = base[:dot]
	}

	tmp := os.TempDir()

	// Resolve gameDir: allow short folder names like "garrysmod" or "hl2" relative to BaseGamePath
	gameDir := config.GameDir
	if gameDir != "" && !filepath.IsAbs(gameDir) && config.BaseGamePath != "" {
		gameDir = filepath.Join(config.BaseGamePath, gameDir)
	}

	// BSP output directory must match the VMF directory. We intentionally ignore any configured BspDir
	// because VBSP should output next to the VMF, and subsequent tools (VVIS/VRAD) must reference that.
	bspDir := dir

	exeDir := config.ExeDir
	if exeDir == "" {
		exeDir = ""
	}

	if config.TmpDir != "" {
		tmp = config.TmpDir
	}

	bsp := filepath.Join(bspDir, name+".bsp")

	vars["$path"] = dir
	vars["$mapdir"] = dir
	vars["$file"] = name
	vars["$name"] = name
	vars["$tmp"] = tmp
	vars["$gamedir"] = gameDir
	vars["$game"] = gameDir
	vars["$bspdir"] = bspDir
	vars["$bsp"] = bsp
	vars["$exedir"] = exeDir
	// Full VMF absolute path
	vars["$vmf"] = abs
	return vars
}

func expandArgs(args []string, vars map[string]string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = expandString(a, vars)
	}
	return out
}

func expandString(s string, vars map[string]string) string {
	if s == "" {
		return s
	}

	// Replace longer keys first to avoid partial overlaps (e.g., $game vs $gamedir)
	order := []string{"$gamedir", "$bspdir", "$mapdir", "$exedir", "$vmf", "$file", "$name", "$path", "$tmp", "$bsp", "$game"}
	res := s
	seen := map[string]bool{}
	for _, k := range order {
		if v, ok := vars[k]; ok {
			seen[k] = true
			res = strings.ReplaceAll(res, k, v)
		}
	}

	for k, v := range vars {
		if !seen[k] {
			res = strings.ReplaceAll(res, k, v)
		}
	}
	return res
}

// resolveProgramPath resolves a configured program path against BaseGamePath when needed.
// Rules:
// - If path is absolute and exists, return it.
// - If BaseGamePath is set:
//   - If path is not absolute, join BaseGamePath with it.
//   - If path is absolute but does not exist, try joining BaseGamePath with the trimmed leading separators.
//
// - Otherwise, return the original path.
func resolveProgramPath(p string) string {
	if p == "" {
		return p
	}
	// Normalize backslashes which may appear in JSON examples
	norm := strings.ReplaceAll(p, "\\", "/")
	// If absolute and exists, use as-is
	if filepath.IsAbs(norm) {
		if _, err := os.Stat(norm); err == nil {
			return norm
		}
	}
	base := config.BaseGamePath
	if base == "" {
		return norm
	}
	// If not absolute, join with base
	if !filepath.IsAbs(norm) {
		return filepath.Join(base, norm)
	}
	// Absolute but missing; treat as relative-to-base with trimmed leading separators
	trimmed := strings.TrimLeft(norm, "/\\")
	return filepath.Join(base, trimmed)
}

package client

import (
	"MapRelay/logging"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var logger = logging.Named("Client")

type compileRequest struct {
	VMF      string `json:"vmf"`
	VMFName  string `json:"vmfName,omitempty"`
	VMFData  []byte `json:"vmfData,omitempty"`
	Preset   string `json:"preset"`
	Password string `json:"password"`
}

type Preset struct {
	Name  string `json:"name"`
	Steps []struct {
		Program string   `json:"program"`
		Args    []string `json:"args"`
	} `json:"steps"`
}

func RunClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	serverUrl := fs.String("server", "localhost:8000", "Server WS URL (host:port). Do not include protocol or trailing slash")
	useHttp := fs.Bool("useHttp", false, "Use HTTP instead of HTTPS")
	vmfPath := fs.String("vmf", "map.vmf", "VMF path")
	preset := fs.String("preset", "default", "Preset name to use")
	password := fs.String("password", "", "Server password, if configured")
	uploadPreset := fs.String("uploadPreset", "", "Path to a preset JSON file to upload/update on server")

	if err := fs.Parse(args); err != nil {
		logger.Fatal("Failed to parse client flags", zap.Error(err))
		return
	}

	if *uploadPreset != "" {
		b, err := os.ReadFile(*uploadPreset)
		if err != nil {
			logger.Fatal("Failed to read preset file", zap.Error(err))
			return
		}

		scheme := "https://"
		if *useHttp {
			scheme = "http://"
		}

		apiUrl := scheme + *serverUrl + "/api/presets"
		req, err := http.NewRequest(http.MethodPost, apiUrl, bytes.NewReader(b))
		if err != nil {
			logger.Fatal("Failed to create request", zap.Error(err))
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Password", *password)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			logger.Fatal("Failed to upload preset", zap.Error(err))
			return
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			logger.Fatal("Upload failed", zap.Int("status", resp.StatusCode), zap.String("body", string(body)))
			return
		}

		logger.Info("Preset uploaded successfully")

		return
	}

	wsUrl := "ws://" + *serverUrl
	c, _, err := websocket.DefaultDialer.Dial(wsUrl, nil)
	if err != nil {
		logger.Fatal("Failed to connect to server", zap.Error(err))
		return
	}

	defer c.Close()

	logger.Info("Uploading VMF", zap.String("path", *vmfPath))
	b, err := os.ReadFile(*vmfPath)
	if err != nil {
		logger.Fatal("Failed to read VMF", zap.Error(err))
		return
	}
	req := compileRequest{VMF: *vmfPath, VMFName: filepath.Base(*vmfPath), VMFData: b, Preset: *preset, Password: *password}
	payload, _ := json.Marshal(req)
	if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
		logger.Fatal("Failed to send VMF to server", zap.Error(err))
		return
	}
	logger.Info("Uploaded VMF", zap.Int("bytes", len(b)))

	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			logger.Error("Failed to read message", zap.Error(err))
			break
		}

		// Try to parse typed JSON first
		var mt struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(msg, &mt); err == nil && mt.Type != "" {
			// Handle BSP transfer specially
			if mt.Type == "bsp" {
				var bm struct {
					Type string `json:"type"`
					Name string `json:"name"`
					Data string `json:"data"`
				}
				if err := json.Unmarshal(msg, &bm); err == nil {
					logger.Info("Downloading compiled BSP", zap.String("name", bm.Name))
					decoded, derr := base64.StdEncoding.DecodeString(bm.Data)
					if derr != nil {
						logger.Error("Failed to decode BSP data", zap.Error(derr))
						continue
					}
					outDir := filepath.Dir(*vmfPath)
					outName := bm.Name
					if outName == "" {
						// derive from vmf name
						base := filepath.Base(*vmfPath)
						name := base
						if dot := len(base) - len(filepath.Ext(base)); dot > 0 {
							name = base[:dot]
						}
						outName = name + ".bsp"
					}
					outPath := filepath.Join(outDir, outName)
					if err := os.WriteFile(outPath, decoded, 0644); err != nil {
						logger.Error("Failed to write BSP file", zap.Error(err), zap.String("path", outPath))
						continue
					}
					logger.Info("Downloaded compiled BSP", zap.String("path", outPath), zap.Int("bytes", len(decoded)))
					continue
				}
			}
			// Other typed messages with optional message field
			var m struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(msg, &m); err == nil {
				logger.Info("Received", zap.String("type", m.Type), zap.String("message", m.Message))
				if m.Type == "done" {
					logger.Info("Compilation done")
					break
				}
				continue
			}
		}

		// Fallback to legacy plain text protocol
		message := string(msg)
		logger.Info("Received message", zap.String("message", message))
		if message == "COMPILE_DONE" {
			logger.Info("Compilation done")
			break
		}
	}
	logger.Info("Client finished")
}

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0o755)
	}
	return nil
}

func getAssetPath(mediaType string) string {
	base := make([]byte, 32)
	_, err := rand.Read(base)
	if err != nil {
		panic("failed to generate random bytes")
	}
	id := base64.RawURLEncoding.EncodeToString(base)

	ext := mediaTypeToExt(mediaType)
	return fmt.Sprintf("%s%s", id, ext)
}

func (cfg apiConfig) getAssetDiskPath(assetPath string) string {
	return filepath.Join(cfg.assetsRoot, assetPath)
}

func (cfg apiConfig) getAssetURL(assetPath string) string {
	return fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, assetPath)
}

func mediaTypeToExt(mediaType string) string {
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 {
		return ".bin"
	}
	return "." + parts[1]
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	// set cmd's Stdout field to a pointer to a new bytes.Buffer
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	// run the Command
	if err := cmd.Run(); err != nil {
		return "", errors.New("could not run the ffprobe command")
	}

	var output struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return "", errors.New("could not unmarshal stdout")
	}

	if len(output.Streams) == 0 {
		return "", errors.New("streams array empty")
	}

	width := output.Streams[0].Width
	height := output.Streams[0].Height

	ratio := float64(width) / float64(height)
	if math.Abs(ratio-(16.0/9.0)) < 0.01 {
		return "16:9", nil
	}
	if math.Abs(ratio-(9.0/16.0)) < 0.01 {
		return "9:16", nil
	}
	return "other", nil
}

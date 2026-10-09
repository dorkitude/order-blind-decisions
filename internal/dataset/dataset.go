// Package dataset downloads and reads the pinned RewardBench 2 test split.
package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
)

const (
	// Revision is the pinned allenai/reward-bench-2 commit.
	Revision = "7ff08853b0d5686e79b13fda8677024f566a104a"
	// File is the test split inside the dataset repository.
	File = "data/test-00000-of-00001.parquet"
	// SHA256 is the expected hash of File at Revision.
	SHA256 = "c8ec60efbd75d2f9dcba4121e6101f7a6015abc38a34e034ae2c7ae886265958"
	// LocalName is where Fetch stores the file, under the data directory.
	LocalName = "upstream/reward-bench-2-test.parquet"
)

// URL is the download location of the pinned file.
var URL = "https://huggingface.co/datasets/allenai/reward-bench-2/resolve/" + Revision + "/" + File

// Row is one RewardBench 2 prompt with its labeled responses.
type Row struct {
	ID         string   `parquet:"id"`
	Prompt     string   `parquet:"prompt"`
	Chosen     []string `parquet:"chosen,list"`
	Rejected   []string `parquet:"rejected,list"`
	NumCorrect int64    `parquet:"num_correct"`
	Subset     string   `parquet:"subset"`
}

// Path returns the local parquet path under dataDir.
func Path(dataDir string) string { return filepath.Join(dataDir, LocalName) }

// Fetch downloads the pinned file into dataDir unless a verified copy exists.
func Fetch(dataDir string) (string, error) {
	p := Path(dataDir)
	if Verify(p) == nil {
		return p, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Get(URL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %d", URL, resp.StatusCode)
	}
	tmp := p + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := Verify(tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return p, os.Rename(tmp, p)
}

// Verify checks a local copy against SHA256.
func Verify(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != SHA256 {
		return fmt.Errorf("%s: sha256 %s, want %s", path, got, SHA256)
	}
	return nil
}

// Load reads every row from a verified local copy.
func Load(path string) ([]Row, error) {
	if err := Verify(path); err != nil {
		return nil, err
	}
	rows, err := parquet.ReadFile[Row](path)
	if err != nil {
		return nil, err
	}
	for i, r := range rows {
		if r.ID == "" || strings.TrimSpace(r.Prompt) == "" || len(r.Chosen) == 0 {
			return nil, fmt.Errorf("row %d (%q) is incomplete", i, r.ID)
		}
	}
	return rows, nil
}

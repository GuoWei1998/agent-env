package agent

import (
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type interrupted struct{}

func (interrupted) Read([]byte) (int, error) { return 0, errors.New("disconnected") }
func TestInterruptedUploadKeepsOriginal(t *testing.T) {
	root, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	if e = root.WriteFile("report", []byte("old"), 0600); e != nil {
		t.Fatal(e)
	}
	req := httptest.NewRequest("PUT", "/files/report", io.MultiReader(strings.NewReader("partial"), interrupted{}))
	req.SetPathValue("path", "report")
	w := httptest.NewRecorder()
	(&Server{root: root}).file(w, req)
	data, e := root.ReadFile("report")
	if w.Code/100 == 2 || e != nil || string(data) != "old" {
		t.Fatal(w.Code, string(data), e)
	}
}

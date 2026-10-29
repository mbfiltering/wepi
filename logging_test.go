package wepi

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// runLogged runs one GET through a fresh controller and returns what was logged.
func runLogged(t *testing.T, logAll bool, status int) string {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags, prevAll := log.Writer(), log.Flags(), LogAllResponses
	log.SetOutput(&buf)
	log.SetFlags(0)
	LogAllResponses = logAll
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags); LogAllResponses = prevAll })

	w := Get()
	AddGET[string](w, "/thing", func(params ParamsManager, req *http.Request) (string, *CustomResponse, error) {
		if status != 0 {
			return "x", Custom().SetStatus(status), nil
		}
		return "ok", nil, nil
	})
	if _, err := w.Run("", httptest.NewRequest(http.MethodGet, "/thing", nil), httptest.NewRecorder()); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestLogAllResponses_DefaultLogsEveryResponse(t *testing.T) {
	if !LogAllResponses {
		t.Fatal("default must stay true so existing users see no change")
	}
	if out := runLogged(t, true, 0); !strings.Contains(out, "/thing response status code: 200") {
		t.Errorf("200 not logged with LogAllResponses=true: %q", out)
	}
}

func TestLogAllResponses_OffSkips2xx(t *testing.T) {
	if out := runLogged(t, false, 0); strings.Contains(out, "response status code") {
		t.Errorf("200 logged with LogAllResponses=false: %q", out)
	}
}

func TestLogAllResponses_OffStillLogsErrors(t *testing.T) {
	for _, st := range []int{400, 404, 500} {
		if out := runLogged(t, false, st); !strings.Contains(out, "response status code") {
			t.Errorf("%d not logged with LogAllResponses=false: %q", st, out)
		}
	}
}

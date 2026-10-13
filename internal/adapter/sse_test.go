package adapter

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSSEKeepAlivePing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	stream := newSSE(c)
	stop := stream.startKeepAlive(15 * time.Millisecond)
	time.Sleep(70 * time.Millisecond)
	stop()
	if !strings.Contains(rec.Body.String(), `"type":"ping"`) {
		t.Fatalf("missing ping: %q", rec.Body.String())
	}
}

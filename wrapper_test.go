// start of ./wrapper_test.go

package o11y

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewHTTPClient(t *testing.T) {
	// Test default transport
	client := NewHTTPClient(nil)
	assert.NotNil(t, client)
	assert.NotNil(t, client.Transport)

	// Test custom transport
	customTr := &http.Transport{MaxIdleConns: 10}
	client2 := NewHTTPClient(customTr)
	assert.NotNil(t, client2)
	assert.NotEqual(t, customTr, client2.Transport, "Transport should be wrapped")
}

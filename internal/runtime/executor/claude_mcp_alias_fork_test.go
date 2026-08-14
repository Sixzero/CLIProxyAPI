package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v7/sdk/translator"
	"github.com/tidwall/gjson"
)

// FORK: MCP tool-name aliasing is off by default here (see
// claudeMCPToolAliasEnabled). Upstream tests that describe the aliasing
// behavior opt back in through this helper, so the suite's default keeps
// matching production. Living in a fork-owned file limits the upstream test
// files to a single added line each.
func enableClaudeMCPToolAliasForTest(t *testing.T) {
	t.Helper()
	t.Setenv("CPA_CLAUDE_MCP_TOOL_ALIAS", "1")
}

func TestPrepareClaudeOAuthToolNamesForUpstream_DisabledByDefaultKeepsToolNames(t *testing.T) {
	body := []byte(`{"model":"claude-opus-4-7","tools":[{"name":"bash"},{"name":"read"}],"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"bash","input":{}}]}]}`)
	out, reverseMap := prepareClaudeOAuthToolNamesForUpstream(body, claudeMCPAliasOptions{secret: "fork-caller"})

	if len(reverseMap) != 0 {
		t.Fatalf("reverse map = %v, want empty", reverseMap)
	}
	for path, want := range map[string]string{
		"tools.0.name":              "bash",
		"tools.1.name":              "read",
		"messages.0.content.0.name": "bash",
	} {
		if got := gjson.GetBytes(out, path).String(); got != want {
			t.Fatalf("%s = %q, want %q", path, got, want)
		}
	}
}

// The three request paths are covered separately: a toggle honoured by the
// helper but bypassed by one call site would still alias tools in production.
func TestClaudeExecutor_ExecuteCloakedOAuthKeepsToolNamesByDefault(t *testing.T) {
	var upstreamName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamName = gjson.GetBytes(body, "tools.0.name").String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4-6","content":[{"type":"tool_use","id":"toolu_1","name":%q,"input":{"query":"go"}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`, upstreamName)
	}))
	defer server.Close()

	auth := &cliproxyauth.Auth{
		ID: "oauth-no-alias",
		Attributes: map[string]string{
			"api_key":  "sk-ant-oat-no-alias",
			"base_url": server.URL,
		},
		Metadata: claudeOAuthTestMetadata(),
	}
	payload := []byte(`{"model":"claude-opus-5","system":"caller-system","messages":[{"role":"user","content":"search"}],"tools":[{"name":"search_web","description":"search","input_schema":{"type":"object"}}]}`)
	resp, errExecute := NewClaudeExecutor(&config.Config{}).Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-opus-5",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude})
	if errExecute != nil {
		t.Fatalf("Execute() error = %v", errExecute)
	}
	if upstreamName != "search_web" {
		t.Fatalf("upstream tool name = %q, want search_web", upstreamName)
	}
	if got := gjson.GetBytes(resp.Payload, "content.0.name").String(); got != "search_web" {
		t.Fatalf("client tool name = %q, want search_web; payload=%s", got, resp.Payload)
	}
}

func TestClaudeExecutor_ExecuteStreamCloakedOAuthKeepsToolNamesByDefault(t *testing.T) {
	var upstreamName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamName = gjson.GetBytes(body, "tools.0.name").String()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":%q,\"input\":{}}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", upstreamName)
	}))
	defer server.Close()

	auth := &cliproxyauth.Auth{
		ID: "oauth-no-alias-stream",
		Attributes: map[string]string{
			"api_key":  "sk-ant-oat-no-alias-stream",
			"base_url": server.URL,
		},
		Metadata: claudeOAuthTestMetadata(),
	}
	payload := []byte(`{"model":"claude-opus-5","system":"caller-system","messages":[{"role":"user","content":"fetch"}],"tools":[{"name":"fetch_url","description":"fetch","input_schema":{"type":"object"}}],"stream":true}`)
	result, errStream := NewClaudeExecutor(&config.Config{}).ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "claude-opus-5",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude})
	if errStream != nil {
		t.Fatalf("ExecuteStream() error = %v", errStream)
	}
	var downstream bytes.Buffer
	for chunk := range result.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
		downstream.Write(chunk.Payload)
	}
	if upstreamName != "fetch_url" {
		t.Fatalf("upstream tool name = %q, want fetch_url", upstreamName)
	}
	if !strings.Contains(downstream.String(), `"name":"fetch_url"`) {
		t.Fatalf("downstream stream missing fetch_url: %s", downstream.String())
	}
}

func TestClaudeExecutor_CountTokensCloakedOAuthKeepsToolNamesByDefault(t *testing.T) {
	var upstreamBody []byte
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		var errRead error
		upstreamBody, errRead = io.ReadAll(req.Body)
		if errRead != nil {
			t.Fatal(errRead)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"input_tokens":34}`)),
			Request:    req,
		}, nil
	})
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", http.RoundTripper(transport))
	auth := &cliproxyauth.Auth{Attributes: map[string]string{"api_key": "sk-ant-oat-count-no-alias"}}
	payload := []byte(`{"model":"claude-opus-5","messages":[{"role":"user","content":[{"type":"text","text":"x"}]}],"tools":[{"name":"search_web","input_schema":{"type":"object"}}]}`)

	if _, errCount := NewClaudeExecutor(&config.Config{}).countTokensUpstream(ctx, auth, cliproxyexecutor.Request{
		Model:   "claude-opus-5",
		Payload: payload,
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude}); errCount != nil {
		t.Fatalf("countTokensUpstream() error = %v", errCount)
	}
	if got := gjson.GetBytes(upstreamBody, "tools.0.name").String(); got != "search_web" {
		t.Fatalf("count_tokens tool name = %q, want search_web", got)
	}
}

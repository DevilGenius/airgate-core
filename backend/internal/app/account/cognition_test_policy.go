package account

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/DevilGenius/airgate-core/internal/plugin"
)

type cognitionTestPolicy struct {
	Enabled bool   `json:"enabled"`
	Prompt  string `json:"prompt"`
	Pattern string `json:"regexp"`
	matcher *regexp.Regexp
}

// ClearCognitionTest restores the untested state without changing other account metadata.
func (s *Service) ClearCognitionTest(ctx context.Context, id int) error {
	if _, err := s.repo.Update(ctx, id, UpdateInput{ClearCognitionTest: true}); err != nil {
		return err
	}
	if s.stateWriter != nil {
		s.stateWriter.RefreshRouteGraphAccount(ctx, id)
	}
	return nil
}

func loadCognitionTestPolicy(ctx context.Context, inst *plugin.PluginInstance, platform string) (cognitionTestPolicy, error) {
	var policy cognitionTestPolicy
	if platform != "openai" {
		return policy, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, _, body, err := inst.HandleHTTPRequest(ctx, http.MethodGet, "accounts/cognition-test-policy", "", nil, nil)
	if err != nil {
		return policy, fmt.Errorf("读取降智检测配置失败: %w", err)
	}
	// Older plugins have no policy endpoint and retain ordinary connectivity tests.
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
		return policy, nil
	}
	if status != http.StatusOK {
		return policy, fmt.Errorf("读取降智检测配置失败: HTTP %d", status)
	}
	if err := json.Unmarshal(body, &policy); err != nil {
		return policy, fmt.Errorf("降智检测配置无效: %w", err)
	}
	if !policy.Enabled {
		return policy, nil
	}
	if strings.TrimSpace(policy.Prompt) == "" || strings.TrimSpace(policy.Pattern) == "" {
		return policy, fmt.Errorf("降智检测 Prompt 和正则不能为空")
	}
	policy.matcher, err = regexp.Compile(policy.Pattern)
	if err != nil {
		return policy, fmt.Errorf("降智检测正则无效: %w", err)
	}
	return policy, nil
}

// Forward bytes unchanged while retaining a bounded copy for completed-text matching.
type cognitionResponseWriter struct {
	http.ResponseWriter
	body     bytes.Buffer
	overflow bool
}

func (w *cognitionResponseWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if w.body.Len()+n <= 4<<20 {
		w.body.Write(p[:n])
	} else {
		w.overflow = true
	}
	return n, err
}
func (w *cognitionResponseWriter) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }
func (w *cognitionResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *cognitionResponseWriter) text() string {
	var text strings.Builder
	for _, line := range bytes.Split(w.body.Bytes(), []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		var event struct {
			Type    string          `json:"type"`
			Delta   json.RawMessage `json:"delta"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))), &event) != nil {
			continue
		}
		if event.Type == "response.output_text.delta" {
			var delta string
			if json.Unmarshal(event.Delta, &delta) == nil {
				text.WriteString(delta)
			}
		} else if len(event.Choices) > 0 && event.Choices[0].Index == 0 {
			text.WriteString(event.Choices[0].Delta.Content)
		}
	}
	return text.String()
}

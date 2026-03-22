package proxy

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// InjectionScript is the feedback widget + remote JS error catcher.
// Gets injected right before </body> in HTML responses.
const InjectionScript = `
<!-- tunr Vibecoder Feedback Widget & Error Catcher -->
<style>
#tunr-feedback-btn {
	position: fixed; bottom: 20px; right: 20px; z-index: 999999;
	background: #6366f1; color: white; border: none; border-radius: 999px;
	padding: 12px 24px; font-family: system-ui, sans-serif; font-weight: 500;
	box-shadow: 0 4px 12px rgba(0,0,0,0.15); cursor: pointer; transition: transform 0.2s;
}
#tunr-feedback-btn:hover { transform: scale(1.05); }
#tunr-feedback-modal {
	display: none; position: fixed; inset: 0; z-index: 999999;
	background: rgba(0,0,0,0.5); align-items: center; justify-content: center;
}
#tunr-feedback-content {
	background: white; padding: 24px; border-radius: 12px; width: 400px;
	box-shadow: 0 10px 25px rgba(0,0,0,0.2); font-family: system-ui;
}
#tunr-feedback-content textarea {
	width: 100%; height: 100px; margin-top: 12px; padding: 12px;
	border: 1px solid #e5e7eb; border-radius: 6px; resize: none; box-sizing: border-box;
}
#tunr-feedback-actions { margin-top: 16px; display: flex; justify-content: flex-end; gap: 8px; }
#tunr-feedback-actions button {
	padding: 8px 16px; border: none; border-radius: 6px; cursor: pointer; font-weight: 500;
}
.tunr-btn-cancel { background: #f3f4f6; color: #4b5563; }
.tunr-btn-submit { background: #6366f1; color: white; }
</style>

<div id="tunr-feedback-modal">
  <div id="tunr-feedback-content">
	<h3 style="margin:0 0 8px 0; color:#111827">Feedback</h3>
	<p style="margin:0; color:#6b7280; font-size:14px">What should we fix on this page?</p>
	<textarea id="tunr-feedback-text" placeholder="e.g. The button color should be blue..."></textarea>
	<div id="tunr-feedback-actions">
	  <button class="tunr-btn-cancel" onclick="document.getElementById('tunr-feedback-modal').style.display='none'">Cancel</button>
	  <button class="tunr-btn-submit" onclick="tunrSubmitFeedback()">Send</button>
	</div>
  </div>
</div>
<button id="tunr-feedback-btn" onclick="document.getElementById('tunr-feedback-modal').style.display='flex'">💬 Feedback</button>

<script>
// Feedback Submit Handler
function tunrSubmitFeedback() {
	const text = document.getElementById('tunr-feedback-text').value;
	if (!text.trim()) return;
	
	const btn = document.querySelector('.tunr-btn-submit');
	btn.innerText = 'Sending...';
	
	fetch('/__tunr/feedback', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({
			message: text,
			url: window.location.href,
			user_agent: navigator.userAgent,
			viewport: window.innerWidth + 'x' + window.innerHeight
		})
	}).then(() => {
		document.getElementById('tunr-feedback-text').value = '';
		document.getElementById('tunr-feedback-modal').style.display = 'none';
		btn.innerText = 'Send';
		alert('Your feedback has been sent to the developer! ✅');
	}).catch(e => {
		btn.innerText = 'Send';
		alert('Failed to send: ' + e.message);
	});
}

// Remote JS Error Catcher
window.addEventListener('error', function(event) {
	fetch('/__tunr/error', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({
			type: 'uncaught_exception',
			message: event.message,
			source: event.filename,
			line: event.lineno,
			col: event.colno,
			url: window.location.href
		})
	}).catch(() => {});
});

window.addEventListener('unhandledrejection', function(event) {
	fetch('/__tunr/error', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({
			type: 'unhandled_rejection',
			message: event.reason ? (event.reason.message || event.reason) : 'Unknown promise rejection',
			url: window.location.href
		})
	}).catch(() => {});
});
</script>
<!-- End tunr Widget -->
`

// injectMiddlewareResponseWriter captures the response body so we can slip our script into HTML.
type injectMiddlewareResponseWriter struct {
	http.ResponseWriter
	bodyBuf    *bytes.Buffer
	statusCode int
	headersSet bool
	isHTML     bool
	gzipped    bool
	canInject  bool
}

func (w *injectMiddlewareResponseWriter) WriteHeader(statusCode int) {
	if w.headersSet {
		return
	}
	w.statusCode = statusCode

	contentType := w.ResponseWriter.Header().Get("Content-Type")
	contentType = strings.ToLower(contentType)
	w.isHTML = strings.Contains(contentType, "text/html")

	encoding := w.ResponseWriter.Header().Get("Content-Encoding")
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	w.gzipped = strings.Contains(encoding, "gzip")
	w.canInject = w.isHTML && (encoding == "" || w.gzipped)
	if w.isHTML && !w.canInject {
		w.ResponseWriter.Header().Set("X-Tunr-Widget-Skipped", "unsupported-content-encoding")
	}

	// Drop Content-Length for HTML — we're about to change the size
	if w.canInject {
		w.ResponseWriter.Header().Del("Content-Length")
	}

	w.headersSet = true
}

func (w *injectMiddlewareResponseWriter) Write(b []byte) (int, error) {
	if !w.headersSet {
		w.WriteHeader(http.StatusOK)
	}

	// Buffer HTML responses in memory so we can inject into them
	if w.canInject {
		return w.bodyBuf.Write(b)
	}

	// Non-HTML (images, css, js) goes straight through
	if w.statusCode > 0 {
		w.ResponseWriter.WriteHeader(w.statusCode)
		w.statusCode = 0
	}
	return w.ResponseWriter.Write(b)
}

func (w *injectMiddlewareResponseWriter) flush() {
	if !w.canInject {
		return
	}
	// Buffered HTML path: if the upstream never wrote a body (204/304/HEAD edge cases),
	// we must still commit status + headers. Otherwise the tunnel sees an empty response.
	if w.bodyBuf.Len() == 0 {
		if w.statusCode > 0 {
			w.ResponseWriter.WriteHeader(w.statusCode)
		}
		return
	}

	// 1. Decompress if gzipped
	var body []byte
	if w.gzipped {
		gz, err := gzip.NewReader(bytes.NewReader(w.bodyBuf.Bytes()))
		if err == nil {
			decoded, err2 := io.ReadAll(gz)
			gz.Close()
			if err2 == nil {
				body = decoded
			} else {
				body = w.bodyBuf.Bytes() // fallback
				w.gzipped = false
			}
		} else {
			body = w.bodyBuf.Bytes()
			w.gzipped = false
		}
	} else {
		body = w.bodyBuf.Bytes()
	}

	// 2. Inject right before </body>
	bodyStr := string(body)
	if strings.Contains(bodyStr, "id=\"tunr-feedback-btn\"") {
		if w.statusCode > 0 {
			w.ResponseWriter.WriteHeader(w.statusCode)
		}
		_, _ = w.ResponseWriter.Write(body)
		return
	}
	idx := strings.LastIndex(strings.ToLower(bodyStr), "</body>")
	if idx != -1 {
		bodyStr = bodyStr[:idx] + InjectionScript + bodyStr[idx:]
	} else {
		// No </body> found — append at the end and hope for the best
		bodyStr += InjectionScript
	}

	modifiedBody := []byte(bodyStr)

	// 3. Re-compress if the original was gzipped
	var finalBody []byte
	if w.gzipped {
		var buf bytes.Buffer
		gzw := gzip.NewWriter(&buf)
		gzw.Write(modifiedBody)
		gzw.Close()
		finalBody = buf.Bytes()
	} else {
		finalBody = modifiedBody
	}

	// 4. Ship it
	// Inline widget needs inline script/style support.
	if w.ResponseWriter.Header().Get("Content-Security-Policy") != "" {
		w.ResponseWriter.Header().Del("Content-Security-Policy")
		w.ResponseWriter.Header().Set("X-Tunr-Widget-CSP", "removed-for-injection")
	}
	w.ResponseWriter.Header().Set("Content-Length", fmt.Sprintf("%d", len(finalBody)))
	if w.statusCode > 0 {
		w.ResponseWriter.WriteHeader(w.statusCode)
	}
	w.ResponseWriter.Write(finalBody)
}

// InjectMiddleware wraps responses to inject the feedback widget and remote error logger.
func InjectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &injectMiddlewareResponseWriter{
			ResponseWriter: w,
			bodyBuf:        &bytes.Buffer{},
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(rw, r)
		rw.flush()
	})
}

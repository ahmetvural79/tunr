package proxy

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// InjectionScript — Feedback widget ve Remote JS Error Catcher scripti.
// </body> kapanışından hemen önce enjekte edilir.
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
	<h3 style="margin:0 0 8px 0; color:#111827">Geri Bildirim</h3>
	<p style="margin:0; color:#6b7280; font-size:14px">Bu sayfada nereyi düzeltelim?</p>
	<textarea id="tunr-feedback-text" placeholder="Örn: Buton rengi mavi olmalı..."></textarea>
	<div id="tunr-feedback-actions">
	  <button class="tunr-btn-cancel" onclick="document.getElementById('tunr-feedback-modal').style.display='none'">İptal</button>
	  <button class="tunr-btn-submit" onclick="tunrSubmitFeedback()">Gönder</button>
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
	btn.innerText = 'Gönderiliyor...';
	
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
		btn.innerText = 'Gönder';
		alert('Geri bildiriminiz Vibecoder\'a (geliştiriciye) ulaştı! ✅');
	}).catch(e => {
		btn.innerText = 'Gönder';
		alert('Gönderilemedi: ' + e.message);
	});
}

// Remote JS Error Catcher (Vibecoder özelliği)
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

// injectMiddlewareResponseWriter — HTTP yanıtını yakalayıp HTML'e script enjekte eden wrapper
type injectMiddlewareResponseWriter struct {
	http.ResponseWriter
	bodyBuf    *bytes.Buffer
	statusCode int
	headersSet bool
	isHTML     bool
	gzipped    bool
}

func (w *injectMiddlewareResponseWriter) WriteHeader(statusCode int) {
	if w.headersSet {
		return
	}
	w.statusCode = statusCode

	contentType := w.ResponseWriter.Header().Get("Content-Type")
	w.isHTML = strings.Contains(strings.ToLower(contentType), "text/html")

	encoding := w.ResponseWriter.Header().Get("Content-Encoding")
	w.gzipped = strings.Contains(encoding, "gzip")

	// Eğer HTML ise, modify edeceğimiz için Content-Length hatalı olur, kaldır
	if w.isHTML {
		w.ResponseWriter.Header().Del("Content-Length")
	}

	w.headersSet = true
}

func (w *injectMiddlewareResponseWriter) Write(b []byte) (int, error) {
	if !w.headersSet {
		w.WriteHeader(http.StatusOK)
	}

	// Sadece HTML yanıtlarını belleğe alıyoruz (modify etmek için)
	if w.isHTML {
		return w.bodyBuf.Write(b)
	}

	// HTML değilse doğrudan yaz (resim, css, js akışı bozulmasın)
	if w.statusCode > 0 {
		w.ResponseWriter.WriteHeader(w.statusCode)
		w.statusCode = 0
	}
	return w.ResponseWriter.Write(b)
}

func (w *injectMiddlewareResponseWriter) flush() {
	if !w.isHTML || w.bodyBuf.Len() == 0 {
		return
	}

	// 1. Gzip decode (eğer sıkıştırılmışsa)
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

	// 2. Enjeksiyon yap (</body> kapanışından hemen önce)
	bodyStr := string(body)
	idx := strings.LastIndex(strings.ToLower(bodyStr), "</body>")
	if idx != -1 {
		bodyStr = bodyStr[:idx] + InjectionScript + bodyStr[idx:]
	} else {
		// </body> bulamazsa sonuna ekle
		bodyStr += InjectionScript
	}

	modifiedBody := []byte(bodyStr)

	// 3. Tekrar Gzip encode (eğer orijinali öyleyse)
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

	// 4. İstemciye gönder
	w.ResponseWriter.Header().Set("Content-Length", fmt.Sprintf("%d", len(finalBody)))
	if w.statusCode > 0 {
		w.ResponseWriter.WriteHeader(w.statusCode)
	}
	w.ResponseWriter.Write(finalBody)
}

// InjectMiddleware — feedback widget'ı ve remote error loglayıcısını enjekte eder
func InjectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &injectMiddlewareResponseWriter{
			ResponseWriter: w,
			bodyBuf:        &bytes.Buffer{},
			statusCode:     http.StatusOK, // varsayılan
		}

		next.ServeHTTP(rw, r)
		rw.flush()
	})
}

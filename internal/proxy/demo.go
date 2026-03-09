package proxy

import (
	"encoding/json"
	"net/http"
)

// DemoMiddleware — API ve form isteklerinde state değişimini engeller.
// Unsafe HTTP metodlarını (POST, PUT, PATCH, DELETE) durdurur, sanki
// başarılı olmuş gibi fake 2xx yanıt döner. 
//
// "Siparişi Tamamla" butonuna basıldığında app çökmez, ama gerçek sipariş de oluşmaz.
func DemoMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sadece GET, HEAD ve OPTIONS isteklerine izin ver
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// Feedback veya error gönderimi her durumda çalışmalı
		if r.URL.Path == "/__tunr/feedback" || r.URL.Path == "/__tunr/error" {
			next.ServeHTTP(w, r)
			return
		}

		// State değiştiren istek engellendi — Fake başarılı yanıt dön
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Tunr-Demo-Mode", "blocked-mutation")
		
		status := http.StatusOK
		if r.Method == http.MethodPost {
			status = http.StatusCreated
		}
		w.WriteHeader(status)

		// Birçok frontend framework (React Query, SWR) JSON bekliyor
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "demo_success",
			"message": "Mutations are disabled in Tunr Demo Mode. Request intercepted and faked.",
			"tunr":   true,
			"method":  r.Method,
			"path":    r.URL.Path,
		})
	})
}

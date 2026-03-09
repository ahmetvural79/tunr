module github.com/tunr-dev/tunr

go 1.22

require (
	// color: terminal renkleri için - siyah/beyaz terminal 2010'da kaldı
	github.com/fatih/color v1.17.0
	// uuid: unique tunnel ID oluşturmak için
	github.com/google/uuid v1.6.0
	// websocket: WebSocket + HMR proxy için (Vite/Next.js sevecek)
	github.com/gorilla/websocket v1.5.3
	// cobra: CLI framework - çünkü flags yazmak acı verici
	github.com/spf13/cobra v1.8.1
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/sys v0.24.0 // indirect
)

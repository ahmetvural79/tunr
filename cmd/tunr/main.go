package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/tunr-dev/tunr/internal/auth"
	"github.com/tunr-dev/tunr/internal/config"
	"github.com/tunr-dev/tunr/internal/daemon"
	"github.com/tunr-dev/tunr/internal/inspector"
	"github.com/tunr-dev/tunr/internal/logger"
	"github.com/tunr-dev/tunr/internal/mcp"
	"github.com/tunr-dev/tunr/internal/tunnel"
	"github.com/spf13/cobra"
)

// Version - goreleaser'ın build zamanında inject ettiği versiyon
// "dev" = local, releases'ta "1.2.3" gibi bir şey olur
var Version = "dev"

func main() {
	// Global panic recovery — beklenmedik crash'leri güzelce yakala
	// Kullanıcı raw "runtime error" görmek istemez
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\n💥 Beklenmedik hata: %v\n", r)
			fmt.Fprintln(os.Stderr, "Lütfen bildirin: https://github.com/tunr-dev/tunr/issues")
			os.Exit(1)
		}
	}()

	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var verbose bool

	root := &cobra.Command{
		Use:     "tunr",
		Short:   "Vibecoder'lar için local → public tunnel aracı",
		Long:    "tunr, local sunucunuzu < 3 saniyede herkese açık URL ile paylaşmanızı sağlar.\nKonfigürasyon yok. Sertifika yok. Sadece çalışır.\n\nBelgeler: https://tunr.sh/docs",
		Version: Version,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if verbose {
				logger.SetLevel(logger.DEBUG)
				logger.Debug("Verbose mod aktif — her şeyi göreceğiz, hazır mısınız?")
			}
		},
	}

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Detaylı log çıktısı (debug modu)")

	root.AddCommand(
		newShareCmd(),
		newStartCmd(),
		newStopCmd(),
		newStatusCmd(),
		newLogsCmd(),
		newDoctorCmd(),
		newLoginCmd(),
		newLogoutCmd(),
		newVersionCmd(),
		newOpenCmd(),
		newReplayCmd(),
		newMCPCmd(),
		newConfigCmd(),
	)

	return root
}

// ─── SHARE ───────────────────────────────────────────────────────────────────

func newShareCmd() *cobra.Command {
	var port int
	var subdomain string
	var noOpen bool

	// Vibecoder Demo Flags
	var demoMode bool
	var freeze bool
	var injectWidget bool
	var autoLogin string

	// Advanced Features
	var password string
	var ttl time.Duration
	var expire time.Duration
	var pathRoutes []string // Örn: /api=3000

	cmd := &cobra.Command{
		Use:   "share",
		Short: "Local sunucuyu anında paylaş",
		Long:  "Local port'u < 3 saniyede public URL olarak paylaşır.\nCtrl+C ile tunnel kapanır.\n\nVibecoder Müşteri Demoları (Pro):\n  --demo                 State değiştiren istekleri (POST vs) engeller\n  --freeze               Localhost çökerse cache'deki son versiyonu sunar\n  --inject-widget        Müşteriye UI feedback widget gömer\n  --auto-login \"tok=1\"   Otomatik cookie enjekte eder",
		Example: `  tunr share --port 3000
  tunr share --port 8080 --subdomain myapp
  
  # Demo Mode (Güvenli Sunum)
  tunr share -p 3000 --demo --freeze --inject-widget`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Sinyal handling — Ctrl+C, kill vb. yakalamak için
			ctx, stop := signal.NotifyContext(cmd.Context(),
				syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()

			cfg, err := config.Load()
			if err != nil {
				logger.Warn("Config yüklenemedi, varsayılanlar kullanılıyor")
				cfg = config.DefaultConfig()
			}

			// Auth token (opsiyonel - anon kullanım da mümkün)
			token, _ := auth.GetToken()
			// GÜVENLİK: token değişkeni sadece aşağıya iletiliyor, log'a geçmiyor

			mgr := tunnel.NewManager("https://relay.tunr.sh")
			mgr.SetAuthToken(token)

			logger.Info("Tunnel başlatılıyor (port %d)...", port)

			// Path routing parsing (örn: /api=3000, /=3000)
			parsedRoutes := make(map[string]int)
			for _, r := range pathRoutes {
				parts := strings.SplitN(r, "=", 2)
				if len(parts) == 2 {
					p, _ := strconv.Atoi(parts[1])
					if p > 0 {
						parsedRoutes[parts[0]] = p
					}
				}
			}

			opts := tunnel.StartOptions{
				Subdomain:    subdomain,
				HTTPS:        cfg.Tunnel.TLSVerify,
				AuthToken:    token,
				
				// Faz 8 Vibecoder
				DemoMode:     demoMode,
				Freeze:       freeze,
				InjectWidget: injectWidget,
				AutoLogin:    autoLogin,

				// Advanced Features
				Password:   password,
				TTL:        ttl,
				PathRoutes: parsedRoutes,
			}
			// Fallback alias for TTL
			if expire > 0 && ttl == 0 {
				opts.TTL = expire
			}
			t, err := mgr.Start(ctx, port, opts)
			if err != nil {
				return fmt.Errorf("tunnel başlatılamadı: %w", err)
			}

			// URL'i güzel göster
			logger.PrintURL(t.PublicURL)

			// İstatistik tablosu
			printShareInfo(t, port)

			// Ctrl+C bekle
			<-ctx.Done()

			fmt.Println()
			logger.Info("Kapatılıyor... (tunnel %s)", t.ID)
			mgr.Remove(t.ID)

			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "LOCAL port numarası (zorunlu)")
	cmd.Flags().StringVar(&subdomain, "subdomain", "", "Özel subdomain (pro feature)")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "URL'yi tarayıcıda otomatik açma")
	
	// Vibecoder Flags
	cmd.Flags().BoolVar(&demoMode, "demo", false, "State değiştiren proxy isteklerini mockla (Sadece okuma)")
	cmd.Flags().BoolVar(&freeze, "freeze", false, "Localhost çökerse cache'den sun (Demo safe)")
	cmd.Flags().BoolVar(&injectWidget, "inject-widget", false, "HTML'e Feedback + Remote Error Catcher widget ekle")
	cmd.Flags().StringVar(&autoLogin, "auto-login", "", "Otomatik login için Cookie enjekte et")

	// Advanced Features
	cmd.Flags().StringVar(&password, "password", "", "Protect tunnel with Basic Auth (format: user:pass or just password)")
	cmd.Flags().DurationVar(&ttl, "ttl", 0, "Automatically close the tunnel after this duration (e.g. 1h, 30m)")
	cmd.Flags().DurationVar(&expire, "expire", 0, "Alias for --ttl")
	cmd.Flags().StringSliceVar(&pathRoutes, "route", nil, "Route paths to different ports (e.g. --route /api=3001 --route /=3000)")

	// GÜVENLİK: Port artık path router varsa opsiyonel olabilir, ama basitlik için required bıraktım
	_ = cmd.MarkFlagRequired("port")

	return cmd
}

// printShareInfo - tunnel aktif olunca kullanışlı bilgileri göster
func printShareInfo(t *tunnel.Tunnel, port int) {
	dim := color.New(color.FgHiBlack)
	cyan := color.New(color.FgCyan)

	fmt.Println()
	dim.Printf("  ┌─────────────────────────────────────────┐\n")
	dim.Printf("  │")
	fmt.Printf("  %-38s", fmt.Sprintf("tunnel ID: %s", cyan.Sprint(t.ID)))
	dim.Printf("│\n")
	dim.Printf("  │")
	fmt.Printf("  %-38s", fmt.Sprintf("local: %s", cyan.Sprint(fmt.Sprintf("http://localhost:%d", port))))
	dim.Printf("│\n")
	dim.Printf("  │")
	fmt.Printf("  %-38s", fmt.Sprintf("başladı: %s", cyan.Sprint(t.StartedAt.Format("15:04:05"))))
	dim.Printf("│\n")
	dim.Printf("  └─────────────────────────────────────────┘\n")
	fmt.Println()
	dim.Println("  Durdurmak için Ctrl+C...")
	fmt.Println()
}

// ─── START (Daemon) ───────────────────────────────────────────────────────────

func newStartCmd() *cobra.Command {
	var port int
	var subdomain string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Tunnel'ı daemon modunda başlat",
		Long:  "Tunnel'ı arka planda çalıştırır. Terminal kapansa bile devam eder.",
		Example: `  tunr start --port 3000
  tunr start --port 8080 --subdomain myapi`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Daemon zaten çalışıyor mu?
			if daemon.IsRunning() {
				return fmt.Errorf("bir daemon zaten çalışıyor. 'tunr status' ile kontrol edin")
			}

			// Port validasyonu
			if port < 1024 || port > 65535 {
				return fmt.Errorf("geçersiz port: %d (1024-65535 arası)", port)
			}

			// PID kaydet
			if err := daemon.WritePID(Version); err != nil {
				logger.Warn("PID kaydedilemedi (daemon takibi çalışmayacak): %v", err)
			}

			// Sinyal handling
			ctx, stop := signal.NotifyContext(cmd.Context(),
				syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()

			// Cleanup: kapanınca PID'i sil
			defer func() {
				if err := daemon.CleanPID(); err != nil {
					logger.Warn("PID temizlenemedi: %v", err)
				}
			}()

			token, _ := auth.GetToken()
			mgr := tunnel.NewManager("https://relay.tunr.sh")
			mgr.SetAuthToken(token)

			t, err := mgr.Start(ctx, port, tunnel.StartOptions{
				Subdomain: subdomain,
				HTTPS:     true,
				AuthToken: token,
			})
			if err != nil {
				return fmt.Errorf("daemon tunnel başlatılamadı: %w", err)
			}

			// Daemon state'e tunnel ekle
			_ = daemon.AddTunnel(daemon.TunnelInfo{
				ID:        t.ID,
				LocalPort: port,
				PublicURL: t.PublicURL,
				StartedAt: t.StartedAt,
			})

			logger.PrintURL(t.PublicURL)
			logger.Info("Daemon modunda çalışıyor (PID %d)", os.Getpid())

			<-ctx.Done()
			mgr.StopAll()

			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "LOCAL port numarası (zorunlu)")
	cmd.Flags().StringVar(&subdomain, "subdomain", "", "Özel subdomain (pro feature)")
	_ = cmd.MarkFlagRequired("port")

	return cmd
}

// ─── STOP ────────────────────────────────────────────────────────────────────

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Aktif daemon'ı durdur",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !daemon.IsRunning() {
				logger.Info("Çalışan daemon yok zaten. Tebrikler?")
				return nil
			}

			if err := daemon.Stop(); err != nil {
				return fmt.Errorf("daemon durdurulamadı: %w", err)
			}

			logger.Info("Daemon durduruldu. Görüşmek üzere! 👋")
			return nil
		},
	}
}

// ─── STATUS ──────────────────────────────────────────────────────────────────

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Daemon ve aktif tunnel durumunu göster",
		RunE: func(cmd *cobra.Command, args []string) error {
			state, err := daemon.ReadPID()
			if err != nil {
				return fmt.Errorf("daemon state okunamadı: %w", err)
			}

			if state == nil || !daemon.IsRunning() {
				logger.Info("Çalışan daemon yok. 'tunr start --port X' ile başlatın.")
				return nil
			}

			cyan := color.New(color.FgCyan, color.Bold)
			dim := color.New(color.FgHiBlack)

			fmt.Println()
			cyan.Printf("  tunr daemon çalışıyor\n")
			dim.Printf("  PID: %d • Başlangıç: %s • Versiyon: %s\n",
				state.PID,
				state.StartedAt.Format("15:04:05"),
				state.Version,
			)
			fmt.Println()

			if len(state.Tunnels) == 0 {
				dim.Println("  Aktif tunnel yok.")
			} else {
				for _, t := range state.Tunnels {
					green := color.New(color.FgGreen, color.Bold)
					fmt.Printf("  %s  %s → %s\n",
						green.Sprint("●"),
						dim.Sprint(fmt.Sprintf(":%d", t.LocalPort)),
						color.New(color.FgGreen, color.Underline).Sprint(t.PublicURL),
					)
				}
			}
			fmt.Println()

			return nil
		},
	}
}

// ─── LOGS ────────────────────────────────────────────────────────────────────

func newLogsCmd() *cobra.Command {
	var follow bool
	var port int

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Gerçek zamanlı request log'larını göster",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO Faz 2: WebSocket stream üzerinden log
			logger.Info("logs komutu Faz 2'de gelecek. Şimdilik 'tunr doctor' deneyin.")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Canlı takip modu (tail -f gibi)")
	cmd.Flags().IntVarP(&port, "port", "p", 0, "Belirli bir port'a ait loglar")

	return cmd
}

// ─── DOCTOR ──────────────────────────────────────────────────────────────────

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Sistem kontrolü yap, sorunları teşhis et",
		Long:  "tunr'nun düzgün çalışması için gerekli her şeyi kontrol eder.\nSorun mu yaşıyorsunuz? Önce bunu çalıştırın.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

func runDoctor() error {
	okStyle   := color.New(color.FgGreen, color.Bold)
	failStyle := color.New(color.FgRed, color.Bold)
	warnStyle := color.New(color.FgYellow, color.Bold)
	titleStyle := color.New(color.FgCyan, color.Bold)

	titleStyle.Println("\n🔍 tunr doctor — Sistem Sağlık Kontrolü")
	color.New(color.FgHiBlack).Println("─────────────────────────────────────────")
	fmt.Println()

	passed, total := 0, 0

	check := func(name string, fn func() (string, bool)) {
		total++
		msg, ok := fn()
		if ok {
			passed++
			fmt.Printf("  %s  %s %s\n", okStyle.Sprint("✓"), name, color.New(color.FgHiBlack).Sprint(msg))
		} else {
			fmt.Printf("  %s  %s %s\n", failStyle.Sprint("✗"), name, warnStyle.Sprint(msg))
		}
	}

	// İnternet
	check("İnternet bağlantısı", func() (string, bool) {
		c := &http.Client{Timeout: 3 * time.Second}
		resp, err := c.Get("https://1.1.1.1")
		if err != nil {
			return "(bağlantı yok)", false
		}
		defer resp.Body.Close()
		return "", true
	})

	// Binary
	check("tunr binary", func() (string, bool) {
		return fmt.Sprintf("(v%s)", Version), true
	})

	// Daemon durumu
	check("Daemon durumu", func() (string, bool) {
		if daemon.IsRunning() {
			state, _ := daemon.ReadPID()
			if state != nil {
				return fmt.Sprintf("(PID %d, %d tunnel aktif)", state.PID, len(state.Tunnels)), true
			}
			return "(çalışıyor)", true
		}
		return "(çalışmıyor — 'tunr start --port X' ile başlat)", false
	})

	// Config
	check("Config dosyası", func() (string, bool) {
		_, err := config.Load()
		if err != nil {
			return "(config yüklenemedi)", false
		}
		dir, _ := config.ConfigDir()
		return fmt.Sprintf("(%s)", dir), true
	})

	// Auth
	check("Kimlik doğrulama", func() (string, bool) {
		if auth.IsAuthenticated() {
			return "(giriş yapılmış ✓)", true
		}
		return "('tunr login' ile giriş yap)", false
	})

	// Relay
	check("Relay (tunr.sh)", func() (string, bool) {
		c := &http.Client{Timeout: 3 * time.Second}
		resp, err := c.Get("https://tunr.sh")
		if err != nil {
			return "(relay erişilemiyor)", false
		}
		defer resp.Body.Close()
		return "(OK)", true
	})

	fmt.Println()
	color.New(color.FgHiBlack).Println("─────────────────────────────────────────")

	if passed == total {
		okStyle.Printf("  Mükemmel! %d/%d kontrol geçti 🎉\n\n", passed, total)
	} else {
		warnStyle.Printf("  %d/%d kontrol geçti. Yukarıdaki ✗ maddelere bak.\n\n", passed, total)
		fmt.Println("  Yardım: https://tunr.sh/docs/troubleshooting")
		fmt.Println("  Issue: https://github.com/tunr-dev/tunr/issues")
		fmt.Println()
	}

	return nil
}

// ─── LOGIN / LOGOUT ──────────────────────────────────────────────────────────

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "tunr hesabına giriş yap (magic link)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// CSRF koruması için state üret (crypto/rand kullanıyor)
			state, err := auth.GenerateState()
			if err != nil {
				return fmt.Errorf("güvenlik state'i üretilemedi: %w", err)
			}
			_ = state

			// TODO Faz 1 devamı: OAuth flow implement et
			// 1. Local callback server başlat (random port)
			// 2. Tarayıcıda tunr.sh/auth?state=...&callback=localhost:PORT aç
			// 3. Token gelince keychain'e yaz
			logger.Info("Magic link giriş akışı Faz 1 devamında gelecek.")
			logger.Info("Şimdilik: https://tunr.sh/early-access adresinden kayıt olun.")
			return nil
		},
	}
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Oturumu kapat, token'ı güvenle sil",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := auth.DeleteToken(); err != nil {
				return fmt.Errorf("logout sırasında hata: %w", err)
			}
			logger.Info("Oturum kapatıldı, token silindi. Görüşmek üzere! 👋")
			return nil
		},
	}
}

// ─── VERSION ─────────────────────────────────────────────────────────────────

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "tunr versiyonunu göster",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("tunr v%s\nhttps://tunr.sh\n", Version)
		},
	}
}
// ─── OPEN ────────────────────────────────────────────────────────────────────

func newOpenCmd() *cobra.Command {
	var dashPort int

	cmd := &cobra.Command{
		Use:   "open",
		Short: "Dashboard'u tarayıcıda aç",
		Long:  "tunr HTTP inspector ve dashboard'u varsayılan tarayıcıda açar.",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := fmt.Sprintf("http://localhost:%d", dashPort)
			logger.Info("Dashboard açılıyor: %s", url)

			// Platform bazında tarayıcıyı aç
			// GÜVENLİK: args hiçbir zaman user input'undan gelmiyor
			// sabit URL kullanıyoruz — command injection riski yok
			var err error
			switch runtime.GOOS {
			case "darwin":
				err = exec.Command("open", url).Start()
			case "windows":
				err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
			default: // linux ve diğerleri
				err = exec.Command("xdg-open", url).Start()
			}

			if err != nil {
				logger.Warn("Tarayıcı açılamadı: %v", err)
				logger.Info("Manuel olarak açın: %s", url)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&dashPort, "port", 19842, "Dashboard portu")
	return cmd
}

// ─── REPLAY ──────────────────────────────────────────────────────────────────

func newReplayCmd() *cobra.Command {
	var localPort int
	var exportCurl bool

	cmd := &cobra.Command{
		Use:   "replay <request-id>",
		Short: "Kaydedilen bir HTTP isteğini tekrar gönder",
		Long:  "Inspector'da yakalanmış bir isteği tekrar local sunucuya gönderir.\ncurl formatında export da yapabilirsiniz.",
		Example: `  tunr replay abc12345
  tunr replay abc12345 --port 3000
  tunr replay abc12345 --curl`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestID := args[0]

			// GÜVENLİK: request ID sadece alfanümerik olmalı
			for _, c := range requestID {
				if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
					return fmt.Errorf("geçersiz request ID formatı: sadece alfanümerik karakterler olabilir")
				}
			}

			// API'den replay iste
			apiURL := fmt.Sprintf("http://localhost:19842/api/v1/requests/%s", requestID)

			if exportCurl {
				resp, err := http.Post(apiURL+"?action=curl", "application/json", nil)
				if err != nil {
					return fmt.Errorf("curl export alınamadı: %w", err)
				}
				defer resp.Body.Close()
				// stdout'a yaz
				_, err = fmt.Fscan(os.Stdout, resp.Body)
				return err
			}

			resp, err := http.Post(
				fmt.Sprintf("%s?action=replay&port=%d", apiURL, localPort),
				"application/json", nil,
			)
			if err != nil {
				return fmt.Errorf("replay gönderilemedi (daemon çalışıyor mu?): %w", err)
			}
			defer resp.Body.Close()

			logger.Info("Replay tamamlandı (status %d)", resp.StatusCode)
			return nil
		},
	}

	cmd.Flags().IntVarP(&localPort, "port", "p", 3000, "Local sunucu portu")
	cmd.Flags().BoolVar(&exportCurl, "curl", false, "curl komutu olarak export et")
	return cmd
}

// ─── MCP ─────────────────────────────────────────────────────────────────────

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "MCP sunucusunu başlat (Claude, Cursor, VS Code için)",
		Long: `Model Context Protocol sunucusunu stdio üzerinden başlatır.
Claude Desktop, Cursor veya Windsurf'ten tunr'yu AI araçlarıyla kullanmanızı sağlar.

Claude Desktop kurulumu için ~/.claude/claude_desktop_config.json'a ekleyin:
  {
    "mcpServers": {
      "tunr": {
        "command": "tunr",
        "args": ["mcp"]
      }
    }
  }

Cursor için .cursor/mcp.json dosyasına ekleyin:
  {
    "mcpServers": {
      "tunr": { "command": "tunr", "args": ["mcp"] }
    }
  }`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Sinyal handling
			ctx, stop := signal.NotifyContext(cmd.Context(),
				syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			// Inspector (opsiyonel — daemon çalışıyorsa bağlanılır)
			ins := inspector.New(1000)

			// MCP server başlat
			server := mcp.New(ins, nil)
			return server.Serve(ctx)
		},
	}
}

// ─── CONFIG ──────────────────────────────────────────────────────────────────

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Proje config dosyasını yönet (.tunr.json)",
		Long:  "Workspace bazlı tunr konfigürasyonunu görüntüle veya düzenle.",
	}

	// tunr config show
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Mevcut config'i göster",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("config yüklenemedi: %w", err)
			}
			data, _ := json.MarshalIndent(cfg, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	})

	// tunr config init
	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Proje için .tunr.json oluştur",
		RunE: func(cmd *cobra.Command, args []string) error {
			defaultCfg := map[string]interface{}{
				"$schema":          "https://tunr.sh/schema/.tunr.schema.json",
				"port":             3000,
				"inspectorEnabled": true,
				"dashboardPort":    19842,
				"mcp":              map[string]bool{"enabled": true},
			}

			data, err := json.MarshalIndent(defaultCfg, "", "  ")
			if err != nil {
				return err
			}

			// Zaten var mı?
			if _, err := os.Stat(".tunr.json"); err == nil {
				return fmt.Errorf(".tunr.json zaten var. Silip tekrar çalıştırın.")
			}

			if err := os.WriteFile(".tunr.json", data, 0644); err != nil {
				return fmt.Errorf(".tunr.json yazılamadı: %w", err)
			}

			logger.Info(".tunr.json oluşturuldu! Editörde düzenleyebilirsiniz.")
			logger.Info("JSON Schema: https://tunr.sh/schema/.tunr.schema.json")
			return nil
		},
	})

	return cmd
}

// Compiler'a bunları göster (lint hataları için)
var _ = context.Background
var _ = json.Marshal
var _ = inspector.New
var _ = mcp.New

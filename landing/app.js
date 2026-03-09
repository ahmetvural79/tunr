/**
 * tunr.sh — Landing Page App Logic
 * 
 * Burada çok süslü şeyler yok, sadece:
 * 1. Terminal typing animasyonu (en önemli kısım)
 * 2. Scroll efektleri (görsel şatafat)
 * 3. Waitlist form (asıl iş)
 * 
 * Vanilla JS kullandık çünkü React için landing page'e
 * 400kb gönderilmez, insani bir şey yapalım.
 */

/* ── Terminal Animasyonu ── */

const TERMINAL_CMD = "tunr share --port 3000";

// Output satırlarını birer birer göster
const OUTPUT_TIMINGS = [
  { id: 0, delay: 200 },   // "Connecting..."
  { id: 1, delay: 600 },   // "Setting up HTTPS..."
  { id: 2, delay: 1000 },  // boş satır
  { id: 3, delay: 1400 },  // URL satırı - para anı
  { id: 4, delay: 1800 },  // boş satır
  { id: 5, delay: 2000 },  // "Ctrl+C..."
];

function startTerminalAnimation() {
  const cmdEl = document.getElementById("t-cmd-1");
  const cursor = document.getElementById("t-cursor-1");
  const output = document.getElementById("t-output");

  if (!cmdEl || !cursor || !output) return; // element yoksa uğraşma

  let charIndex = 0;
  const typeSpeed = 60; // ms per character

  // Yazma animasyonu
  const typeInterval = setInterval(() => {
    if (charIndex < TERMINAL_CMD.length) {
      cmdEl.textContent = TERMINAL_CMD.slice(0, charIndex + 1);
      charIndex++;
    } else {
      // Komut yazıldı, output'u göster
      clearInterval(typeInterval);
      cursor.style.display = "none"; // cursor'ı gizle, gerçekmiş gibi

      setTimeout(() => {
        output.classList.remove("hidden");
        const lines = output.querySelectorAll(".output-line");

        OUTPUT_TIMINGS.forEach(({ id, delay }) => {
          const line = lines[id];
          if (!line) return;
          setTimeout(() => {
            line.classList.add("show");
          }, delay);
        });
      }, 200);
    }
  }, typeSpeed);
}

// Sayfa yüklenince başlat, 1 saniye sonra (user'ın hazır olması için)
window.addEventListener("DOMContentLoaded", () => {
  setTimeout(startTerminalAnimation, 800);
});

/* ── Nav Scroll Effect ── */

const nav = document.getElementById("nav");
let lastScrollY = 0;

window.addEventListener("scroll", () => {
  const currentScrollY = window.scrollY;

  // 50px scroll sonrası nav'ı blur yap
  if (currentScrollY > 50) {
    nav.classList.add("scrolled");
  } else {
    nav.classList.remove("scrolled");
  }

  lastScrollY = currentScrollY;
}, { passive: true }); // passive: true = scroll performansını artırır

/* ── Scroll Reveal Animasyonu ── */

// IntersectionObserver: element viewport'a girince animasyon tetikle
// requestAnimationFrame veya setInterval'dan çok daha verimli
const revealObserver = new IntersectionObserver(
  (entries) => {
    entries.forEach((entry) => {
      if (entry.isIntersecting) {
        entry.target.classList.add("visible");
        revealObserver.unobserve(entry.target); // bir kere göster, yeter
      }
    });
  },
  {
    threshold: 0.1,     // elementin %10'u görününce tetikle
    rootMargin: "0px",
  }
);

// "reveal" class'ına sahip her elementi observe et
document.querySelectorAll(".reveal").forEach((el) => {
  revealObserver.observe(el);
});

/* ── Waitlist Form ── */

const form = document.getElementById("waitlist-form");
const emailInput = document.getElementById("waitlist-email");
const submitBtn = document.getElementById("waitlist-submit");
const btnText = document.getElementById("btn-text");
const btnLoading = document.getElementById("btn-loading");
const formMessage = document.getElementById("form-message");

/**
 * Email validation - RFC 5321'e göre değil, pratik hayata göre.
 * Regex'e güven ama aşırı güvenme, backend'de de doğrula.
 * 
 * GÜVENLİK: Email sadece format validate ediliyor.
 * Backend'de gerçek doğrulama yapılmalı (injection vb.).
 */
function isValidEmail(email) {
  if (!email || typeof email !== "string") return false;
  if (email.length > 254) return false; // RFC 5321 max
  const re = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  return re.test(email.trim());
}

/**
 * Honeypot kontrolü - bot mu, insan mı?
 * Bots genellikle tüm input'ları doldurur.
 * "website" alanı CSS ile gizlenmiş, gerçek kullanıcı görmez.
 */
function isBot() {
  const honeypot = form.querySelector('input[name="website"]');
  return honeypot && honeypot.value.length > 0;
}

function showMessage(text, type) {
  formMessage.textContent = text;
  formMessage.className = `form-message ${type}`;
  formMessage.classList.remove("hidden");
}

function setLoading(loading) {
  if (loading) {
    btnText.classList.add("hidden");
    btnLoading.classList.remove("hidden");
    submitBtn.disabled = true;
    emailInput.disabled = true;
  } else {
    btnText.classList.remove("hidden");
    btnLoading.classList.add("hidden");
    submitBtn.disabled = false;
    emailInput.disabled = false;
  }
}

form && form.addEventListener("submit", async (e) => {
  e.preventDefault();

  // Gizli mesaj: bot yakalandı
  if (isBot()) {
    // Bota başarılı mesajı göster ama hiçbir şey yapma
    // (biraz acımasız ama bots hak ediyor)
    showMessage("Kaydınız alındı! ✨", "success");
    return;
  }

  const email = emailInput.value.trim();

  // Client-side validation - UX için
  if (!isValidEmail(email)) {
    showMessage("Geçerli bir e-posta adresi girin.", "error");
    emailInput.focus();
    return;
  }

  setLoading(true);
  formMessage.classList.add("hidden");

  try {
    /**
     * GÜVENLİK: CSRF için header ekle.
     * Backend henüz yok, fetch simüle ediyoruz.
     * Gerçek implementasyonda:
     * - CSRF token header ekle
     * - Rate limiting backend'de kontrol edilmeli (IP başına X/dakika)
     * - Email normalize et (küçük harf, trim)
     */

    // TODO: Gerçek API endpoint'e bağla (Faz 0'da)
    // const res = await fetch("https://api.tunr.sh/waitlist", {
    //   method: "POST",
    //   headers: {
    //     "Content-Type": "application/json",
    //     "X-CSRF-Token": getCsrfToken(),
    //   },
    //   body: JSON.stringify({ email: email.toLowerCase() }),
    // });

    // Şimdilik simülasyon - 1.5 saniye bekle, başarılı de
    await new Promise((resolve) => setTimeout(resolve, 1500));

    showMessage(
      "🎉 Kaydınız alındı! Beta açıldığında ilk sizi haberdar edeceğiz.",
      "success"
    );

    form.reset();

    // Başarı sonrası formu gizle (opsiyonel, güzel görünür)
    setTimeout(() => {
      form.style.display = "none";
    }, 3000);

  } catch (err) {
    // Gerçek bir network hatası oldu
    showMessage(
      "Bir hata oluştu. Lütfen tekrar deneyin veya GitHub'da issue açın.",
      "error"
    );
    console.error("Waitlist submit error:", err);
  } finally {
    setLoading(false);
  }
});

/* ── Smooth Scroll for Anchor Links ── */

document.querySelectorAll('a[href^="#"]').forEach((link) => {
  link.addEventListener("click", (e) => {
    const targetId = link.getAttribute("href").slice(1);
    const target = document.getElementById(targetId);

    if (target) {
      e.preventDefault();
      const navHeight = nav ? nav.offsetHeight : 80;
      const top = target.getBoundingClientRect().top + window.scrollY - navHeight - 24;
      window.scrollTo({ top, behavior: "smooth" });
    }
  });
});

/* ── Klavye Erişilebilirliği ── */

// ESC ile form'u reset et (küçük ama güzel detay)
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && document.activeElement === emailInput) {
    emailInput.blur();
  }
});

/* ── Feature card giriş animasyonları ── */

// Feature kartlarının sırayla girmesi için stagger effect
document.querySelectorAll(".feature-card").forEach((card, i) => {
  card.style.transitionDelay = `${i * 80}ms`;
  revealObserver.observe(card);
});

// Step'ler de gözlemlensin
document.querySelectorAll(".step").forEach((step, i) => {
  step.classList.add("reveal");
  step.style.transitionDelay = `${i * 150}ms`;
  revealObserver.observe(step);
});

// Stat kartları
document.querySelectorAll(".stat-card").forEach((card, i) => {
  card.classList.add("reveal");
  card.style.transitionDelay = `${i * 100}ms`;
  revealObserver.observe(card);
});

console.log(
  "%c tunr.sh ",
  "background: linear-gradient(135deg, #00d4ff, #7c3aed); color: white; font-weight: bold; padding: 4px 8px; border-radius: 4px;",
  "\n\nMerhaba meraklı developer! 👋\nKoda bakmak istiyorsanız → https://github.com/tunr-dev/tunr\nKatkı yapmak ister misiniz? PRs welcome!"
);

/* ── Theme Toggle (Light / Dark Mode) ── */

(function initTheme() {
  const stored = localStorage.getItem('tunr-theme');
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  const theme = stored || (prefersDark ? 'dark' : 'light');
  applyTheme(theme);
})();

function applyTheme(theme) {
  if (theme === 'light') {
    document.documentElement.setAttribute('data-theme', 'light');
  } else {
    document.documentElement.removeAttribute('data-theme');
  }
  // Update button icons
  const iconDark = document.querySelector('.theme-icon-dark');
  const iconLight = document.querySelector('.theme-icon-light');
  if (iconDark && iconLight) {
    iconDark.style.display = theme === 'light' ? 'none' : 'inline';
    iconLight.style.display = theme === 'light' ? 'inline' : 'none';
  }
  localStorage.setItem('tunr-theme', theme);
}

document.getElementById('theme-toggle') && document.getElementById('theme-toggle').addEventListener('click', () => {
  const current = localStorage.getItem('tunr-theme') || 'dark';
  applyTheme(current === 'dark' ? 'light' : 'dark');
});

/* ── Feedback Modal ── */

const feedbackBtn = document.getElementById('feedback-btn');
const feedbackOverlay = document.getElementById('feedback-overlay');
const feedbackClose = document.getElementById('feedback-close');
const feedbackSubmit = document.getElementById('feedback-submit');
const feedbackSuccess = document.getElementById('feedback-success');
let activeFeedbackType = 'bug';

feedbackBtn && feedbackBtn.addEventListener('click', () => {
  feedbackOverlay.classList.add('open');
  document.getElementById('feedback-msg').focus();
});

feedbackClose && feedbackClose.addEventListener('click', closeFeedbackModal);

feedbackOverlay && feedbackOverlay.addEventListener('click', (e) => {
  if (e.target === feedbackOverlay) closeFeedbackModal();
});

document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && feedbackOverlay && feedbackOverlay.classList.contains('open')) {
    closeFeedbackModal();
  }
});

document.querySelectorAll('.fb-tab').forEach(tab => {
  tab.addEventListener('click', () => {
    document.querySelectorAll('.fb-tab').forEach(t => t.classList.remove('active'));
    tab.classList.add('active');
    activeFeedbackType = tab.dataset.type;
  });
});

feedbackSubmit && feedbackSubmit.addEventListener('click', () => {
  const msg = document.getElementById('feedback-msg').value.trim();
  const email = document.getElementById('feedback-email').value.trim();
  if (!msg) { document.getElementById('feedback-msg').focus(); return; }

  // Save to localStorage (for admin demo panel)
  const feedbacks = JSON.parse(localStorage.getItem('tunr-feedbacks') || '[]');
  feedbacks.unshift({
    id: Date.now(),
    type: activeFeedbackType,
    message: msg,
    email: email || null,
    status: 'open',
    date: new Date().toISOString(),
  });
  localStorage.setItem('tunr-feedbacks', JSON.stringify(feedbacks.slice(0, 100)));

  // Show success
  feedbackSuccess.style.display = 'block';
  feedbackSubmit.style.display = 'none';
  document.getElementById('feedback-msg').value = '';
  document.getElementById('feedback-email').value = '';
  setTimeout(closeFeedbackModal, 2000);
});

function closeFeedbackModal() {
  feedbackOverlay && feedbackOverlay.classList.remove('open');
  feedbackSuccess && (feedbackSuccess.style.display = 'none');
  feedbackSubmit && (feedbackSubmit.style.display = '');
}

/* ── Cookie Consent Banner ── */

(function initCookieBanner() {
  const consent = localStorage.getItem('tunr-cookie-consent');
  if (consent) return; // Zaten onaylandi

  const banner = document.getElementById('cookie-banner');
  if (!banner) return;

  // Sayfa yüklenince kısa bir süre sonra göster
  setTimeout(() => banner.classList.add('visible'), 1500);

  document.getElementById('cookie-accept') && document.getElementById('cookie-accept').addEventListener('click', () => {
    localStorage.setItem('tunr-cookie-consent', 'accepted');
    banner.classList.remove('visible');
    setTimeout(() => banner.remove(), 500);
  });

  document.getElementById('cookie-reject') && document.getElementById('cookie-reject').addEventListener('click', () => {
    localStorage.setItem('tunr-cookie-consent', 'rejected');
    // İşlevsel olmayan çerezleri devre dışı bırak (şu an yok, ileride analytics için)
    banner.classList.remove('visible');
    setTimeout(() => banner.remove(), 500);
  });
})();


/**
 * tunr.sh — Landing Page App Logic
 * 
 * Nothing too fancy here, just:
 * 1. Terminal typing animation (the main attraction)
 * 2. Scroll effects (visual flair)
 * 3. Waitlist form (the real deal)
 * 
 * We used vanilla JS because shipping 400kb of React
 * for a landing page is not a reasonable thing to do.
 */

/* ── Terminal Animation ── */

const TERMINAL_CMD = "tunr share --port 3000";

// Show output lines one by one
const OUTPUT_TIMINGS = [
  { id: 0, delay: 200 },   // "Connecting..."
  { id: 1, delay: 600 },   // "Setting up HTTPS..."
  { id: 2, delay: 1000 },  // blank line
  { id: 3, delay: 1400 },  // URL line - the money shot
  { id: 4, delay: 1800 },  // blank line
  { id: 5, delay: 2000 },  // "Ctrl+C..."
];

function startTerminalAnimation() {
  const cmdEl = document.getElementById("t-cmd-1");
  const cursor = document.getElementById("t-cursor-1");
  const output = document.getElementById("t-output");

  if (!cmdEl || !cursor || !output) return; // bail if elements missing

  let charIndex = 0;
  const typeSpeed = 60; // ms per character

  // Typing animation
  const typeInterval = setInterval(() => {
    if (charIndex < TERMINAL_CMD.length) {
      cmdEl.textContent = TERMINAL_CMD.slice(0, charIndex + 1);
      charIndex++;
    } else {
      // Command typed, show output
      clearInterval(typeInterval);
      cursor.style.display = "none"; // hide cursor for realism

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

// Start after page load, with a short delay so the user is ready
window.addEventListener("DOMContentLoaded", () => {
  setTimeout(startTerminalAnimation, 800);
});

/* ── Nav Scroll Effect ── */

const nav = document.getElementById("nav");
let lastScrollY = 0;

window.addEventListener("scroll", () => {
  const currentScrollY = window.scrollY;

  // Add blur to nav after 50px of scroll
  if (currentScrollY > 50) {
    nav.classList.add("scrolled");
  } else {
    nav.classList.remove("scrolled");
  }

  lastScrollY = currentScrollY;
}, { passive: true }); // passive: true = improves scroll performance

/* ── Scroll Reveal Animation ── */

// IntersectionObserver: trigger animation when element enters viewport
// Much more efficient than requestAnimationFrame or setInterval
const revealObserver = new IntersectionObserver(
  (entries) => {
    entries.forEach((entry) => {
      if (entry.isIntersecting) {
        entry.target.classList.add("visible");
        revealObserver.unobserve(entry.target); // reveal once, that's enough
      }
    });
  },
  {
    threshold: 0.1,     // trigger when 10% of the element is visible
    rootMargin: "0px",
  }
);

// Observe every element with the "reveal" class
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
 * Email validation — practical, not strictly RFC 5321.
 * Trust the regex but don't over-rely on it; always validate on the backend too.
 * 
 * SECURITY: Only format validation here.
 * Real validation (injection etc.) must happen on the backend.
 */
function isValidEmail(email) {
  if (!email || typeof email !== "string") return false;
  if (email.length > 254) return false; // RFC 5321 max
  const re = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  return re.test(email.trim());
}

/**
 * Honeypot check — bot or human?
 * Bots typically fill in every input field.
 * The "website" field is hidden with CSS; real users never see it.
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

  // Secret: bot caught
  if (isBot()) {
    // Show a success message to the bot but do nothing
    // (a bit ruthless, but bots deserve it)
    showMessage("You're signed up! ✨", "success");
    return;
  }

  const email = emailInput.value.trim();

  // Client-side validation — for UX
  if (!isValidEmail(email)) {
    showMessage("Please enter a valid email address.", "error");
    emailInput.focus();
    return;
  }

  setLoading(true);
  formMessage.classList.add("hidden");

  try {
    /**
     * SECURITY: Add CSRF header.
     * No backend yet, so we simulate the fetch.
     * In the real implementation:
     * - Add a CSRF token header
     * - Rate limiting must be enforced on the backend (X requests/minute per IP)
     * - Normalize the email (lowercase, trim)
     */

    // TODO: Wire up to the real API endpoint (Phase 0)
    // const res = await fetch("https://api.tunr.sh/waitlist", {
    //   method: "POST",
    //   headers: {
    //     "Content-Type": "application/json",
    //     "X-CSRF-Token": getCsrfToken(),
    //   },
    //   body: JSON.stringify({ email: email.toLowerCase() }),
    // });

    // Simulation for now — wait 1.5s and report success
    await new Promise((resolve) => setTimeout(resolve, 1500));

    showMessage(
      "🎉 You're signed up! We'll let you know first when the beta launches.",
      "success"
    );

    form.reset();

    // Hide the form after success (optional, looks nice)
    setTimeout(() => {
      form.style.display = "none";
    }, 3000);

  } catch (err) {
    // An actual network error occurred
    showMessage(
      "Something went wrong. Please try again or open a GitHub issue.",
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

/* ── Keyboard Accessibility ── */

// Reset form on ESC (small but nice detail)
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && document.activeElement === emailInput) {
    emailInput.blur();
  }
});

/* ── Feature Card Entrance Animations ── */

// Stagger effect for feature cards entering sequentially
document.querySelectorAll(".feature-card").forEach((card, i) => {
  card.style.transitionDelay = `${i * 80}ms`;
  revealObserver.observe(card);
});

// Observe steps as well
document.querySelectorAll(".step").forEach((step, i) => {
  step.classList.add("reveal");
  step.style.transitionDelay = `${i * 150}ms`;
  revealObserver.observe(step);
});

// Stat cards
document.querySelectorAll(".stat-card").forEach((card, i) => {
  card.classList.add("reveal");
  card.style.transitionDelay = `${i * 100}ms`;
  revealObserver.observe(card);
});

console.log(
  "%c tunr.sh ",
  "background: linear-gradient(135deg, #00d4ff, #7c3aed); color: white; font-weight: bold; padding: 4px 8px; border-radius: 4px;",
  "\n\nHey there, curious developer! 👋\nWant to peek at the code? → https://github.com/ahmetvural79/tunr\nWant to contribute? PRs welcome!"
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
  if (consent) return; // Already responded

  const banner = document.getElementById('cookie-banner');
  if (!banner) return;

  // Show shortly after page load
  setTimeout(() => banner.classList.add('visible'), 1500);

  document.getElementById('cookie-accept') && document.getElementById('cookie-accept').addEventListener('click', () => {
    localStorage.setItem('tunr-cookie-consent', 'accepted');
    banner.classList.remove('visible');
    setTimeout(() => banner.remove(), 500);
  });

  document.getElementById('cookie-reject') && document.getElementById('cookie-reject').addEventListener('click', () => {
    localStorage.setItem('tunr-cookie-consent', 'rejected');
    // Disable non-essential cookies (none right now, for future analytics)
    banner.classList.remove('visible');
    setTimeout(() => banner.remove(), 500);
  });
})();


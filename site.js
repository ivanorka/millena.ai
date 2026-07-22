let siteLanguage = "hr";

function setSiteLocalized(node, hr, en) {
  if (!node) return;
  node.dataset.hr = hr;
  node.dataset.en = en;
  node.textContent = siteLanguage === "en" ? en : hr;
}

function applyHonestSiteCopy() {
  setSiteLocalized(
    document.querySelector(".hero-kicker"),
    "Vi dajete smjer. Millena vodi lokalni rad od konteksta do provjerene sandbox isporuke.",
    "You set direction. Millena runs the local workflow from context to verified sandbox delivery.",
  );
  setSiteLocalized(
    document.querySelector(".hero-text"),
    "Aplikacija pamti dogovoreno, vodi sadržaj, kalendar i publiku. WhatsApp, Telegram i stvarne društvene objave uključuju se tek kroz provjerene OAuth/webhook adaptere.",
    "The app retains decisions and manages content, calendar, and audience. WhatsApp, Telegram, and live social publishing require verified OAuth/webhook adapters.",
  );
  setSiteLocalized(
    document.querySelector(".intro-section .setup-story article:nth-child(3) p"),
    "Svih osam mreža možete konfigurirati u lokalnom sandboxu; stvarni računi i web/CRM pozivi zahtijevaju provider adapter i upotrebljive tokene.",
    "All eight networks can be configured in the local sandbox; live accounts and web/CRM calls require provider adapters and usable tokens.",
  );
  setSiteLocalized(
    document.querySelector(".companion-copy > p:last-of-type"),
    "Razgovor u aplikaciji je trajan i koristi stvarni projektni kontekst. Telegram i WhatsApp prikazani su kao kanali za budući webhook adapter, ne kao već aktivni botovi.",
    "In-app conversations persist and use real project context. Telegram and WhatsApp are shown as channels for a future webhook adapter, not as already active bots.",
  );
  setSiteLocalized(
    document.querySelector(".bot-message.millena p"),
    "Pripremila sam skice za osam mreža, blog i newsletter. Raspored i sandbox rezultati spremaju se u projekt; vanjsku objavu uključujemo nakon povezivanja providera.",
    "I prepared drafts for eight networks, the blog, and newsletter. Scheduling and sandbox results are stored in the project; live publishing starts after a provider is connected.",
  );
  setSiteLocalized(
    document.querySelector(".channels-section .section-heading > p:last-of-type"),
    "Sadržaj se prilagođava svim podržanim kanalima u jednoj bazi. Lokalni sandbox radi odmah, a stvarni WhatsApp, Telegram, društvene mreže i CMS trebaju zasebne adaptere.",
    "Content is adapted for every supported channel in one database. The local sandbox works immediately; live WhatsApp, Telegram, social networks, and CMS require separate adapters.",
  );
  setSiteLocalized(
    document.querySelector(".web-copy > p:last-of-type"),
    "Aplikacija već ima blog editor, newsletter publiku, analitiku i lokalni web preview. Izravna objava na vanjski web uključuje se kroz ugovoreni CMS adapter.",
    "The app already includes a blog editor, newsletter audience, analytics, and local website preview. Direct publishing to an external site requires an agreed CMS adapter.",
  );
  setSiteLocalized(
    document.querySelector(".trust-section .section-heading > p:last-of-type"),
    "Pravilo pregleda određuje nastaje li skica, zapis za obavezni pregled ili odobren sadržaj. Vanjski provider i dalje mora biti zasebno povezan.",
    "The review rule determines whether a draft, mandatory-review item, or approved item is created. An external provider must still be connected separately.",
  );
  setSiteLocalized(
    document.querySelector(".faq-list article:nth-child(4) p"),
    "Ne za lokalni sandbox. Za stvarne mreže koristi se OAuth/provider aplikacija, a vlastiti web, CRM ili poseban sustav koristi odgovarajući API ili webhook token.",
    "Not for the local sandbox. Live networks use an OAuth/provider application, while a website, CRM, or custom system uses the appropriate API or webhook token.",
  );
  setSiteLocalized(
    document.querySelector(".faq-list article:nth-child(5) p"),
    "Da. Pravilo može stvoriti običnu skicu, zapis za obavezni pregled ili odobren sadržaj; sandbox isporuka i vanjska objava ostaju odvojeni koraci.",
    "Yes. A rule can create a regular draft, a mandatory-review item, or approved content; sandbox delivery and live publishing remain separate steps.",
  );
  setSiteLocalized(document.querySelector(".mini-setup small"), "12 lokalnih konfiguracija", "12 local configurations");
  setSiteLocalized(document.querySelector(".mini-content-row:first-child small"), "Sandbox · 09:42", "Sandbox · 09:42");
  setSiteLocalized(document.querySelector(".mini-automation header strong"), "Primjer sandbox automatizacije", "Sandbox automation example");
  setSiteLocalized(document.querySelector(".mini-automation > div:first-of-type strong"), "Testni događaj spremljen", "Test event stored");
  setSiteLocalized(document.querySelector(".mini-automation > div:first-of-type small"), "Lokalni sandbox", "Local sandbox");
  setSiteLocalized(document.querySelector(".mini-automation > div:last-of-type strong"), "Urednička provjera", "Editorial review");
  setSiteLocalized(document.querySelector(".mini-automation > div:last-of-type small"), "Primjer", "Example");
  setSiteLocalized(document.querySelector(".floating-status.status-one small"), "Sandbox ulaz", "Sandbox input");
  setSiteLocalized(document.querySelector(".floating-status.status-one strong"), "Testni događaj spremljen", "Test event stored");
  setSiteLocalized(document.querySelector(".floating-status.status-two small"), "Sandbox obrada", "Sandbox processing");
  const floatingProvider = document.querySelector(".floating-status.status-two strong");
  if (floatingProvider) floatingProvider.textContent = "LinkedIn · lokalno 12:00";
  setSiteLocalized(document.querySelector(".agent-roster article:first-child strong"), "Agent za kontekst", "Context agent");
  setSiteLocalized(document.querySelector(".agent-roster article:first-child small"), "Projektni izvori i urednička provjera", "Project sources and editorial review");
  setSiteLocalized(document.querySelector(".demo-source .chat-card small"), "Sandbox unos · 09:42", "Sandbox input · 09:42");
  setSiteLocalized(document.querySelector(".demo-source h4"), "Kontekst za uredničku provjeru", "Context for editorial review");
  setSiteLocalized(document.querySelector(".demo-publish .quality-score small"), "Sandbox skica", "Sandbox draft");
  setSiteLocalized(document.querySelector(".demo-publish li:first-child span"), "Činjenice čeka urednik", "Facts await editor review");
  setSiteLocalized(document.querySelector(".demo-publish > button span"), "Otvori sandbox objavu", "Open sandbox post");
  setSiteLocalized(document.querySelector(".send-panel > button span"), "Zakaži sandbox slanje", "Schedule sandbox delivery");
  setSiteLocalized(document.querySelector(".send-panel > div small"), "Prema odabranom terminu", "Based on the selected time");
  setSiteLocalized(document.querySelector(".web-browser form > span"), "Demo obrasca za pretplatu.", "Subscription form demo.");
  const signupDemoInput = document.querySelector(".web-browser form input");
  if (signupDemoInput) {
    signupDemoInput.readOnly = true;
    signupDemoInput.setAttribute("aria-readonly", "true");
    signupDemoInput.dataset.placeholderHr = "Demo · ime@tvrtka.hr";
    signupDemoInput.dataset.placeholderEn = "Demo · name@company.com";
  }
  const signupDemoButton = document.querySelector(".web-browser form button");
  if (signupDemoButton) {
    signupDemoButton.dataset.ariaHr = "Otvori demo publike";
    signupDemoButton.dataset.ariaEn = "Open audience demo";
  }
}

function refreshSiteIcons() {
  if (window.lucide?.createIcons) {
    window.lucide.createIcons({ attrs: { "aria-hidden": "true" } });
  }
}

function applySiteLanguage(language) {
  siteLanguage = language === "en" ? "en" : "hr";
  document.documentElement.lang = siteLanguage;

  document.querySelectorAll("[data-hr][data-en]").forEach((element) => {
    element.textContent = element.dataset[siteLanguage];
  });

  document.querySelectorAll("[data-aria-hr][data-aria-en]").forEach((element) => {
    element.setAttribute("aria-label", element.dataset[`aria${siteLanguage === "hr" ? "Hr" : "En"}`]);
  });

  document.querySelectorAll("[data-placeholder-hr][data-placeholder-en]").forEach((element) => {
    element.placeholder = element.dataset[`placeholder${siteLanguage === "hr" ? "Hr" : "En"}`];
  });

  document.querySelectorAll(".language-button").forEach((button) => {
    const isActive = button.dataset.lang === siteLanguage;
    button.classList.toggle("active", isActive);
    button.setAttribute("aria-pressed", String(isActive));
  });

  document.title = document.body.classList.contains("login-page")
    ? (siteLanguage === "hr" ? "Prijava | Millena AI" : "Sign in | Millena AI")
    : (siteLanguage === "hr" ? "Millena AI | Lokalni sadržajni workspace" : "Millena AI | Local content workspace");

  refreshSiteIcons();
}

document.querySelectorAll(".language-button").forEach((button) => {
  button.addEventListener("click", () => {
    applySiteLanguage(button.dataset.lang);
    const activeMode = document.querySelector("[data-auth-mode].active");
    if (activeMode) setAuthMode(activeMode.dataset.authMode);
  });
});

const menuButton = document.querySelector(".menu-button");
const navLinks = document.querySelector(".nav-links");

menuButton?.addEventListener("click", () => {
  const isOpen = navLinks?.classList.toggle("open") || false;
  menuButton.setAttribute("aria-expanded", String(isOpen));
  menuButton.setAttribute("aria-label", siteLanguage === "hr"
    ? (isOpen ? "Zatvori izbornik" : "Otvori izbornik")
    : (isOpen ? "Close menu" : "Open menu"));
});

navLinks?.querySelectorAll("a").forEach((link) => {
  link.addEventListener("click", () => navLinks.classList.remove("open"));
});

const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
const parallaxItems = [
  { element: document.querySelector(".hero-texture"), section: document.querySelector(".hero-section"), speed: 0.055 },
  { element: document.querySelector(".web-backdrop img"), section: document.querySelector(".web-section"), speed: 0.09 },
  { element: document.querySelector(".closing-section > img"), section: document.querySelector(".closing-section"), speed: 0.075 },
].filter((item) => item.element && item.section);

let motionFrame = 0;

function updatePageMotion() {
  motionFrame = 0;
  document.querySelector(".site-header")?.classList.toggle("scrolled", window.scrollY > 18);
  if (reducedMotion.matches) return;

  const mobileFactor = window.innerWidth < 700 ? 0.55 : 1;
  parallaxItems.forEach(({ element, section, speed }) => {
    const bounds = section.getBoundingClientRect();
    if (bounds.bottom < -120 || bounds.top > window.innerHeight + 120) return;
    const distanceFromCenter = (window.innerHeight / 2) - (bounds.top + bounds.height / 2);
    const offset = Math.max(-52, Math.min(52, distanceFromCenter * speed * mobileFactor));
    element.style.setProperty("--parallax-offset", `${offset.toFixed(1)}px`);
  });
}

function requestPageMotion() {
  if (motionFrame) return;
  motionFrame = window.requestAnimationFrame(updatePageMotion);
}

window.addEventListener("scroll", requestPageMotion, { passive: true });
window.addEventListener("resize", requestPageMotion, { passive: true });
requestPageMotion();

const revealObserver = "IntersectionObserver" in window
  ? new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          entry.target.classList.add("in-view");
          revealObserver.unobserve(entry.target);
        }
      });
    }, { threshold: 0.12 })
  : null;

document.querySelectorAll(".reveal").forEach((element) => {
  if (revealObserver) revealObserver.observe(element);
  else element.classList.add("in-view");
});

document.querySelectorAll("[data-demo]").forEach((button) => {
  button.addEventListener("click", () => {
    const demo = button.dataset.demo;

    document.querySelectorAll("[data-demo]").forEach((tab) => {
      const isActive = tab === button;
      tab.classList.toggle("active", isActive);
      tab.setAttribute("aria-selected", String(isActive));
    });

    document.querySelectorAll("[data-demo-panel]").forEach((panel) => {
      panel.classList.toggle("active", panel.dataset.demoPanel === demo);
    });

    requestAnimationFrame(refreshSiteIcons);
  });
});

document.querySelectorAll(".faq-list article").forEach((item) => {
  const button = item.querySelector("button");
  button?.addEventListener("click", () => {
    const willOpen = !item.classList.contains("open");

    document.querySelectorAll(".faq-list article").forEach((other) => {
      other.classList.remove("open");
      const otherButton = other.querySelector("button");
      const otherIcon = otherButton?.querySelector("svg");
      otherButton?.setAttribute("aria-expanded", "false");
      if (otherIcon) otherIcon.setAttribute("data-lucide", "plus");
    });

    item.classList.toggle("open", willOpen);
    button.setAttribute("aria-expanded", String(willOpen));
    const icon = button.querySelector("svg");
    if (icon) icon.setAttribute("data-lucide", willOpen ? "minus" : "plus");
    refreshSiteIcons();
  });
});

const loginForm = document.querySelector("#login-form");
const authMessage = document.querySelector("#auth-message");
let authMode = "login";

const apiBase = window.location.port === "8000"
  ? `${window.location.protocol}//${window.location.hostname}:8080/api/v1`
  : "/api/v1";

async function siteAPI(path, options = {}) {
  const response = await fetch(`${apiBase}${path}`, {
    ...options,
    credentials: "include",
    headers: {
      Accept: "application/json",
      ...(options.body ? { "Content-Type": "application/json" } : {}),
      ...options.headers,
    },
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error?.message || `API request failed (${response.status})`);
  return payload.data;
}

function showAuthMessage(message, success = false) {
  if (!authMessage) return;
  authMessage.textContent = message;
  authMessage.hidden = !message;
  authMessage.classList.toggle("success", success);
}

function setAuthMode(mode) {
  authMode = mode === "register" ? "register" : "login";
  document.querySelectorAll("[data-auth-mode]").forEach((button) => button.classList.toggle("active", button.dataset.authMode === authMode));
  const registerFields = document.querySelector("#register-fields");
  if (registerFields) registerFields.hidden = authMode !== "register";
  registerFields?.querySelectorAll("input").forEach((input) => { input.required = authMode === "register"; });
  const submitLabel = loginForm?.querySelector(".login-submit span");
  if (submitLabel) {
    submitLabel.textContent = authMode === "register"
      ? (siteLanguage === "hr" ? "Kreiraj Unlimited račun" : "Create Unlimited account")
      : (siteLanguage === "hr" ? "Prijavi se" : "Sign in");
  }
  showAuthMessage("");
}

async function enterApp(target = "app.html") {
  try {
    await siteAPI("/auth/me");
    window.location.href = target;
  } catch {
    const next = target.startsWith("app.html") ? target : "app.html";
    window.location.href = `login.html?next=${encodeURIComponent(next)}`;
  }
}

[
  [".mini-top button", "app.html#content"],
  [".bot-actions button:first-child", "app.html#companion"],
  [".bot-actions button:last-child", "app.html#content"],
  [".demo-toolbar button", "app.html#social"],
  [".demo-publish > button", "app.html#social"],
  [".send-panel > button", "app.html#newsletter"],
  [".web-browser form button", "app.html#audience"],
].forEach(([selector, target]) => document.querySelector(selector)?.addEventListener("click", (event) => {
  event.preventDefault();
  enterApp(target);
}));

loginForm?.addEventListener("submit", async (event) => {
  event.preventDefault();
  const submit = loginForm.querySelector(".login-submit");
  const label = submit?.querySelector("span");
  if (submit) submit.disabled = true;
  if (label) label.textContent = siteLanguage === "hr" ? "Provjeravam..." : "Checking...";
  showAuthMessage("");
  const form = new FormData(loginForm);
  const path = authMode === "register" ? "/auth/register" : "/auth/login";
  const body = authMode === "register"
    ? {
        displayName: String(form.get("displayName") || ""),
        organizationName: String(form.get("organizationName") || ""),
        email: String(form.get("email") || ""),
        password: String(form.get("password") || ""),
      }
    : { email: String(form.get("email") || ""), password: String(form.get("password") || "") };
  try {
    await siteAPI(path, { method: "POST", body: JSON.stringify(body) });
    showAuthMessage(authMode === "register"
      ? (siteLanguage === "hr" ? "Organizacija je kreirana. Otvaram aplikaciju..." : "Organization created. Opening the app...")
      : (siteLanguage === "hr" ? "Prijava uspješna. Otvaram aplikaciju..." : "Signed in. Opening the app..."), true);
    const requested = new URLSearchParams(window.location.search).get("next") || "app.html";
    const target = requested.startsWith("app.html") ? requested : "app.html";
    window.location.href = target;
  } catch (error) {
    showAuthMessage(error.message);
  } finally {
    if (submit) submit.disabled = false;
    if (label) {
      label.textContent = authMode === "register"
        ? (siteLanguage === "hr" ? "Kreiraj Unlimited račun" : "Create Unlimited account")
        : (siteLanguage === "hr" ? "Prijavi se" : "Sign in");
    }
  }
});

document.querySelectorAll("[data-enter-app]").forEach((button) => {
  button.addEventListener("click", (event) => {
    event.preventDefault();
    enterApp(button.dataset.appTarget || "app.html");
  });
});

document.querySelectorAll("[data-auth-mode]").forEach((button) => button.addEventListener("click", () => setAuthMode(button.dataset.authMode)));
document.querySelector("[data-register-toggle]")?.addEventListener("click", (event) => {
  event.preventDefault();
  setAuthMode("register");
  document.querySelector('#register-fields input')?.focus();
});
document.querySelector(".password-toggle")?.addEventListener("click", (event) => {
  const input = document.querySelector('input[name="password"]');
  if (!input) return;
  const showPassword = input.type === "password";
  input.type = showPassword ? "text" : "password";
  event.currentTarget.setAttribute("aria-label", siteLanguage === "hr"
    ? (showPassword ? "Sakrij lozinku" : "Prikaži lozinku")
    : (showPassword ? "Hide password" : "Show password"));
  const icon = event.currentTarget.querySelector("svg");
  if (icon) icon.setAttribute("data-lucide", showPassword ? "eye-off" : "eye");
  refreshSiteIcons();
});

applyHonestSiteCopy();
applySiteLanguage("hr");
setAuthMode("login");
refreshSiteIcons();

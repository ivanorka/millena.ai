const screenNames = {
  overview: { hr: "Pregled", en: "Overview" },
  setup: { hr: "Postavljanje projekta", en: "Project setup" },
  content: { hr: "Sadržaj", en: "Content" },
  calendar: { hr: "Kalendar", en: "Calendar" },
  automations: { hr: "Automatizacije", en: "Automations" },
  social: { hr: "Društvene mreže", en: "Social studio" },
  blog: { hr: "Blog", en: "Blog" },
  newsletter: { hr: "Newsletter", en: "Newsletter" },
  channels: { hr: "Kanali", en: "Channels" },
  audience: { hr: "Publika", en: "Audience" },
  website: { hr: "Web stranica", en: "Website" },
  settings: { hr: "Projekt", en: "Project" },
};

const messages = {
  hr: {
    saved: "Promjena je uspješno spremljena.",
    connected: "Veza je uspješno provjerena.",
    scheduled: "Sadržaj je dodan u raspored.",
    contact: "Kontakt je dodan na newsletter listu.",
    website: "Zahtjev za web prijedlog je spremljen.",
    setup: "Projekt je postavljen i automatizacija je aktivna.",
    rewritten: "Tekst je prerađen prema tonu projekta.",
  },
  en: {
    saved: "Your change was saved successfully.",
    connected: "The connection was verified successfully.",
    scheduled: "Content was added to the schedule.",
    contact: "The contact was added to the newsletter list.",
    website: "The website proposal request was saved.",
    setup: "The project is set up and automation is active.",
    rewritten: "The copy was rewritten to match the project tone.",
  },
};

const validScreens = new Set(Object.keys(screenNames));
const appScreens = [...document.querySelectorAll(".app-screen")];
const navItems = [...document.querySelectorAll(".nav-item")];
const screenTitle = document.querySelector("#screen-title");
const sidebar = document.querySelector("#sidebar");
const sidebarOverlay = document.querySelector("#sidebar-overlay");
const contactModal = document.querySelector("#contact-modal");
const toast = document.querySelector("#toast");
const toastMessage = document.querySelector("#toast-message");
const languageButtons = [...document.querySelectorAll(".language-button")];

let currentLanguage = "hr";
let currentScreen = "overview";
let currentSetupStep = 1;
let toastTimer;

function refreshIcons() {
  if (window.lucide?.createIcons) {
    window.lucide.createIcons({ attrs: { "aria-hidden": "true" } });
  }
}

function applyLanguage(language) {
  if (!messages[language]) return;

  currentLanguage = language;
  document.documentElement.lang = language;
  document.title = language === "hr" ? "Millena IQ" : "Millena IQ";

  document.querySelectorAll("[data-hr][data-en]").forEach((element) => {
    const nextText = element.dataset[language];
    if (typeof nextText === "string") {
      element.textContent = nextText;
    }
  });

  document.querySelectorAll("[data-placeholder-hr][data-placeholder-en]").forEach((element) => {
    element.placeholder = element.dataset[`placeholder${language === "hr" ? "Hr" : "En"}`];
  });

  document.querySelectorAll("[data-aria-hr][data-aria-en]").forEach((element) => {
    element.setAttribute("aria-label", element.dataset[`aria${language === "hr" ? "Hr" : "En"}`]);
  });

  languageButtons.forEach((button) => {
    const isActive = button.dataset.lang === language;
    button.classList.toggle("active", isActive);
    button.setAttribute("aria-pressed", String(isActive));
  });

  updateScreenTitle();
  updateSetupControls();
  refreshIcons();
}

function updateScreenTitle() {
  const title = screenNames[currentScreen]?.[currentLanguage] || screenNames.overview[currentLanguage];
  if (screenTitle) screenTitle.textContent = title;
}

function closeMobileMenu() {
  sidebar?.classList.remove("open");
  sidebarOverlay?.classList.remove("open");
  document.body.style.overflow = "";
}

function navigateTo(screen, options = {}) {
  const destination = validScreens.has(screen) ? screen : "overview";
  currentScreen = destination;

  appScreens.forEach((section) => {
    section.classList.toggle("active", section.dataset.screen === destination);
  });

  navItems.forEach((item) => {
    item.classList.toggle("active", item.dataset.screenTarget === destination);
  });

  updateScreenTitle();
  closeMobileMenu();

  if (options.updateHash !== false) {
    history.replaceState(null, "", `#${destination}`);
  }

  if (options.scroll !== false) {
    window.scrollTo({ top: 0, behavior: "smooth" });
  }

  requestAnimationFrame(refreshIcons);
}

function showToast(type = "saved") {
  if (!toast || !toastMessage) return;

  toastMessage.textContent = messages[currentLanguage][type] || messages[currentLanguage].saved;
  toast.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => toast.classList.remove("show"), 3200);
}

function openContactModal() {
  if (!contactModal) return;
  contactModal.classList.add("open");
  contactModal.setAttribute("aria-hidden", "false");
  document.body.style.overflow = "hidden";
  contactModal.querySelector("input")?.focus();
}

function closeContactModal() {
  if (!contactModal) return;
  contactModal.classList.remove("open");
  contactModal.setAttribute("aria-hidden", "true");
  document.body.style.overflow = "";
}

function updateSetupControls() {
  const setupPanels = [...document.querySelectorAll("[data-setup-panel]")];
  const setupSteps = [...document.querySelectorAll("[data-setup-step]")];
  const setupBack = document.querySelector("#setup-back");
  const setupNext = document.querySelector("#setup-next");
  const setupStepLabel = document.querySelector("#setup-step-label");
  const progress = document.querySelector("#setup-progress-bar");

  setupPanels.forEach((panel) => {
    panel.classList.toggle("active", Number(panel.dataset.setupPanel) === currentSetupStep);
  });

  setupSteps.forEach((step) => {
    const stepNumber = Number(step.dataset.setupStep);
    step.classList.toggle("active", stepNumber === currentSetupStep);
    step.classList.toggle("complete", stepNumber < currentSetupStep);
    const number = step.querySelector(":scope > button > span");
    if (number) number.textContent = stepNumber < currentSetupStep ? "✓" : String(stepNumber);
  });

  if (setupBack) setupBack.disabled = currentSetupStep === 1;
  if (setupStepLabel) setupStepLabel.textContent = `${currentSetupStep} / 5`;
  if (progress) progress.style.width = `${currentSetupStep * 20}%`;

  if (setupNext) {
    const label = setupNext.querySelector("span");
    if (label) {
      label.textContent = currentSetupStep === 5
        ? (currentLanguage === "hr" ? "Aktiviraj projekt" : "Activate project")
        : (currentLanguage === "hr" ? "Nastavi" : "Continue");
    }
  }

  refreshIcons();
}

document.querySelectorAll("[data-screen-target]").forEach((button) => {
  button.addEventListener("click", () => navigateTo(button.dataset.screenTarget));
});

languageButtons.forEach((button) => {
  button.addEventListener("click", () => applyLanguage(button.dataset.lang));
});

document.querySelector("#menu-open")?.addEventListener("click", () => {
  sidebar?.classList.add("open");
  sidebarOverlay?.classList.add("open");
  document.body.style.overflow = "hidden";
});

document.querySelector("#menu-close")?.addEventListener("click", closeMobileMenu);
sidebarOverlay?.addEventListener("click", closeMobileMenu);

document.querySelectorAll("[data-setup-step]").forEach((step) => {
  step.querySelector("button")?.addEventListener("click", () => {
    currentSetupStep = Number(step.dataset.setupStep);
    updateSetupControls();
  });
});

document.querySelector("#setup-back")?.addEventListener("click", () => {
  currentSetupStep = Math.max(1, currentSetupStep - 1);
  updateSetupControls();
});

document.querySelector("#setup-next")?.addEventListener("click", () => {
  if (currentSetupStep < 5) {
    currentSetupStep += 1;
    updateSetupControls();
    document.querySelector(".setup-card")?.scrollIntoView({ behavior: "smooth", block: "start" });
    return;
  }

  showToast("setup");
  window.setTimeout(() => navigateTo("overview"), 550);
});

document.querySelectorAll("[data-strategy-mode]").forEach((button) => {
  button.addEventListener("click", () => {
    document.querySelectorAll("[data-strategy-mode]").forEach((choice) => {
      const isSelected = choice === button;
      choice.classList.toggle("selected", isSelected);
      const icon = choice.querySelector(":scope > svg");
      if (icon) icon.setAttribute("data-lucide", isSelected ? "circle-check" : "circle");
    });

    const questions = document.querySelector(".strategy-questions");
    if (questions) questions.hidden = button.dataset.strategyMode === "upload";
    showToast("saved");
    refreshIcons();
  });
});

document.querySelectorAll(".select-chips button").forEach((button) => {
  button.addEventListener("click", () => button.classList.toggle("active"));
});

document.querySelectorAll(".segmented button").forEach((button) => {
  button.addEventListener("click", () => {
    button.parentElement.querySelectorAll("button").forEach((sibling) => sibling.classList.toggle("active", sibling === button));
  });
});

document.querySelectorAll(".channel-tabs button").forEach((button) => {
  button.addEventListener("click", () => {
    button.parentElement.querySelectorAll("button").forEach((sibling) => sibling.classList.toggle("active", sibling === button));
  });
});

document.querySelectorAll(".connect-action").forEach((button) => {
  button.addEventListener("click", () => {
    const label = button.querySelector("span") || button;
    label.textContent = currentLanguage === "hr" ? "Povezano" : "Connected";
    button.classList.add("connected-action");
    showToast("connected");
  });
});

document.querySelectorAll(".publish-action").forEach((button) => {
  button.addEventListener("click", () => showToast("scheduled"));
});

document.querySelector(".rewrite-button")?.addEventListener("click", () => {
  const editor = document.querySelector(".post-editor");
  if (editor) editor.animate([{ opacity: 0.45 }, { opacity: 1 }], { duration: 420, easing: "ease-out" });
  showToast("rewritten");
});

document.querySelectorAll(".add-contact-action").forEach((button) => button.addEventListener("click", openContactModal));
document.querySelectorAll(".modal-close").forEach((button) => button.addEventListener("click", closeContactModal));
document.querySelector(".modal-save")?.addEventListener("click", () => {
  closeContactModal();
  showToast("contact");
});

contactModal?.addEventListener("click", (event) => {
  if (event.target === contactModal) closeContactModal();
});

document.querySelector(".request-website")?.addEventListener("click", () => showToast("website"));

document.querySelectorAll(".block-grid button").forEach((button) => {
  button.addEventListener("click", () => showToast("saved"));
});

document.addEventListener("keydown", (event) => {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    document.querySelector(".search-box input")?.focus();
  }

  if (event.key === "Escape") {
    closeContactModal();
    closeMobileMenu();
  }
});

window.addEventListener("hashchange", () => {
  const hashScreen = window.location.hash.slice(1);
  if (validScreens.has(hashScreen)) navigateTo(hashScreen, { updateHash: false });
});

applyLanguage("hr");
updateSetupControls();

const initialScreen = window.location.hash.slice(1);
navigateTo(validScreens.has(initialScreen) ? initialScreen : "overview", {
  updateHash: !validScreens.has(initialScreen),
  scroll: false,
});

refreshIcons();

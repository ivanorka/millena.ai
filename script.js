const screenNames = {
  overview: { hr: "Pregled", en: "Overview" },
  setup: { hr: "Postavljanje projekta", en: "Project setup" },
  content: { hr: "Sadržaj", en: "Content" },
  calendar: { hr: "Kalendar", en: "Calendar" },
  automations: { hr: "Automatizacije", en: "Automations" },
  companion: { hr: "Millena bot", en: "Millena bot" },
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
    setup: "Projekt je postavljen.",
    rewritten: "Tekst je prerađen prema tonu projekta.",
    socialConnected: "Sandbox račun je povezan i spremljen u bazu.",
    socialTested: "Sandbox veza je uspješno testirana.",
    socialDisconnected: "Sandbox račun je odspojen.",
    socialPublished: "Testna objava je obrađena i spremljena u bazu.",
    socialRequired: "Prvo povežite sandbox račun za odabranu mrežu.",
    socialError: "Social API nije dovršio zahtjev. Pokušajte ponovno.",
    calendarSaved: "Kalendarska stavka je spremljena u bazu.",
    calendarDeleted: "Kalendarska stavka je obrisana.",
    calendarError: "Kalendar nije dovršio zahtjev. Pokušajte ponovno.",
    actionRecorded: "Akcija je izvršena i zabilježena u audit logu.",
    contentSaved: "Sadržaj je spremljen u bazu.",
    contentDeleted: "Sadržaj je trajno obrisan iz baze.",
    contentGenerated: "AI je generirao nacrt koristeći strategiju projekta.",
    contentRefined: "AI je doradio vaš tekst bez promjene ključnih činjenica.",
    contentError: "Content API nije dovršio zahtjev. Pokušajte ponovno.",
    contentAIError: "AI nije dovršio obradu. Provjerite tekst i pokušajte ponovno.",
    strategySaved: "Strateški kontekst je spremljen i aktivan za AI.",
    strategyFileSaved: "Tekst strategije je izdvojen iz datoteke i spremljen.",
    strategyError: "Strategiju nije bilo moguće spremiti ili obraditi.",
  },
  en: {
    saved: "Your change was saved successfully.",
    connected: "The connection was verified successfully.",
    scheduled: "Content was added to the schedule.",
    contact: "The contact was added to the newsletter list.",
    website: "The website proposal request was saved.",
    setup: "Project setup is complete.",
    rewritten: "The copy was rewritten to match the project tone.",
    socialConnected: "The sandbox account was connected and stored in the database.",
    socialTested: "The sandbox connection was tested successfully.",
    socialDisconnected: "The sandbox account was disconnected.",
    socialPublished: "The test post was processed and stored in the database.",
    socialRequired: "Connect a sandbox account for the selected network first.",
    socialError: "The social API did not complete the request. Please try again.",
    calendarSaved: "The calendar item was saved to the database.",
    calendarDeleted: "The calendar item was deleted.",
    calendarError: "The calendar did not complete the request. Please try again.",
    actionRecorded: "The action was completed and recorded in the audit log.",
    contentSaved: "Content was saved to the database.",
    contentDeleted: "Content was permanently deleted from the database.",
    contentGenerated: "AI generated a draft using the project strategy.",
    contentRefined: "AI refined your copy without changing its key facts.",
    contentError: "The content API did not complete the request. Please try again.",
    contentAIError: "AI did not complete the operation. Check the copy and try again.",
    strategySaved: "The strategy context is saved and active for AI.",
    strategyFileSaved: "Strategy text was extracted from the file and saved.",
    strategyError: "The strategy could not be saved or processed.",
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
  document.title = language === "hr" ? "Millena AI | Aplikacija" : "Millena AI | App";

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
  const requested = validScreens.has(screen) ? screen : "overview";
  const denied = typeof window.__millenaAuthorizeScreen === "function" && !window.__millenaAuthorizeScreen(requested);
  const destination = denied
    ? "overview"
    : requested;
  currentScreen = destination;

  appScreens.forEach((section) => {
    section.classList.toggle("active", section.dataset.screen === destination);
  });

  navItems.forEach((item) => {
    item.classList.toggle("active", item.dataset.screenTarget === destination);
  });

  updateScreenTitle();
  closeMobileMenu();

  if (options.updateHash !== false || denied) {
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
  }
});

document.querySelectorAll("[data-strategy-mode]").forEach((button) => {
  button.addEventListener("click", () => {
    document.querySelectorAll("[data-strategy-mode]").forEach((choice) => {
      const isSelected = choice === button;
      choice.classList.toggle("selected", isSelected);
      choice.setAttribute("aria-pressed", String(isSelected));
      const icon = choice.querySelector(":scope > svg");
      if (icon) icon.setAttribute("data-lucide", isSelected ? "circle-check" : "circle");
    });

    const questions = document.querySelector(".strategy-questions");
    const upload = document.querySelector(".strategy-upload");
    const isUpload = button.dataset.strategyMode === "upload";
    if (questions) questions.hidden = isUpload;
    if (upload) upload.hidden = !isUpload;
    refreshIcons();
  });
});

document.querySelector("#strategy-file")?.addEventListener("change", (event) => {
  const file = event.currentTarget.files?.[0];
  const status = document.querySelector(".strategy-file-status");
  const fileName = document.querySelector(".strategy-file-name");
  if (!status || !fileName) return;

  status.hidden = !file;
  fileName.textContent = file?.name || "";
  refreshIcons();
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
    if (!button.dataset.socialChannel) return;
    button.parentElement.querySelectorAll("button").forEach((sibling) => sibling.classList.toggle("active", sibling === button));
  });
});

document.querySelectorAll(".add-contact-action").forEach((button) => button.addEventListener("click", openContactModal));
document.querySelectorAll(".modal-close").forEach((button) => button.addEventListener("click", closeContactModal));

contactModal?.addEventListener("click", (event) => {
  if (event.target === contactModal) closeContactModal();
});

document.querySelectorAll(".platform-list").forEach((group) => {
  group.querySelectorAll("button").forEach((button) => {
    button.addEventListener("click", () => {
      if (button.getAttribute("aria-disabled") === "true") return;
      group.querySelectorAll("button").forEach((item) => item.classList.toggle("selected", item === button));
    });
  });
});

document.querySelectorAll(".inspector-tabs").forEach((group) => {
  group.querySelectorAll("button").forEach((button) => {
    button.addEventListener("click", () => {
      group.querySelectorAll("button").forEach((item) => item.classList.toggle("active", item === button));
    });
  });
});

const logoutAPIBase = window.location.port === "8000"
  ? `${window.location.protocol}//${window.location.hostname}:8080/api/v1`
  : "/api/v1";

document.querySelectorAll("[data-logout]").forEach((button) => {
  button.addEventListener("click", async () => {
    try {
      await fetch(`${logoutAPIBase}/auth/logout`, { method: "POST", credentials: "include" });
    } finally {
      window.location.href = "login.html";
    }
  });
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

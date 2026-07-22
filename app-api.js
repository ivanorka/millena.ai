(() => {
  const apiStatus = document.querySelector("#api-status");
  const apiBase = window.location.port === "8000"
    ? `${window.location.protocol}//${window.location.hostname}:8080/api/v1`
    : "/api/v1";
  const assetObjectURLs = new Map();

  let projectID = "";
  let revision = 0;
  let hydrated = false;
  let saveTimer = 0;
  let saveInFlight = false;
  let saveQueued = false;
  let socialConnections = new Map();
  let socialPosts = [];
  let projectAssets = [];
  let pendingAssistantAssets = [];
  let socialMediaAssets = [];
  let blogMediaAssets = [];
  let activeSocialContentID = "";
  let activeSocialSourceID = "";
  let activeSocialChannel = "linkedin";
  let socialNewRecordSelected = false;
  let socialVariants = [];
  let socialVariantSaveQueue = Promise.resolve();
  let socialDraftDirty = false;
  let socialEditVersion = 0;
  let socialHydrationToken = 0;
  const socialPublicationTimers = new Map();
  let socialDraftTimer = 0;
  let pendingSocialProvider = "";
  let sessionUser = null;
  let projectAccess = null;
  let calendarItems = [];
  let calendarView = "week";
  let calendarCursor = startOfWeek(new Date());
  let calendarLoadToken = 0;
  let contentItems = [];
  let contentKind = "all";
  let contentStatus = "";
  let contentSearch = "";
  let projectStrategy = null;
  let projectPersonas = [];
  let contentAIStatus = null;
  let strategySaveTimer = 0;
  let strategyChangeVersion = 0;
  let strategySaveQueue = Promise.resolve();
  let strategySavePending = 0;
  let strategyUploadToken = 0;
  let projectProfile = null;
  let dashboardData = null;
  let automationRules = [];
  let channelConnections = [];
  let audienceLists = [];
  let audienceContacts = [];
  let audienceStats = null;
  let audienceSearchTimer = 0;
  let audienceLoadToken = 0;
  let activeAudienceListID = "";
  let assistantStatus = null;
  let assistantThreads = [];
  let assistantMessages = [];
  let activeAssistantThreadID = "";
  let assistantChannel = "app";
  let newsletterDeliveries = [];
  let newsletterContentID = "";
  let blogContentID = "";
  let newsletterDraftIsNew = false;
  let newsletterBlockSelection = [];
  let newsletterDirty = false;
  let blogDirty = false;
  let serviceRequests = [];
  let teamMembers = [];
  let plans = [];
  let entitlement = null;
  let profileSaveTimer = 0;
  let profileChangeVersion = 0;
  let profileSaveQueue = Promise.resolve();

  function statusCopy(status) {
    const copy = {
      hr: {
        connecting: "Spajanje na bazu…",
        connected: "API i baza spojeni",
        saving: "Spremanje u bazu…",
        error: "API nije dostupan",
      },
      en: {
        connecting: "Connecting to database…",
        connected: "API and database connected",
        saving: "Saving to database…",
        error: "API is unavailable",
      },
    };
    return copy[currentLanguage]?.[status] || copy.hr[status];
  }

  function setAPIStatus(status) {
    if (!apiStatus) return;
    apiStatus.className = `api-status ${status}`;
    const label = apiStatus.querySelector("span");
    if (label) label.textContent = statusCopy(status);
  }

  async function apiRequest(path, options = {}) {
    const isFormData = options.body instanceof FormData;
    const response = await fetch(`${apiBase}${path}`, {
      ...options,
      credentials: "include",
      headers: {
        Accept: "application/json",
        ...(options.body && !isFormData ? { "Content-Type": "application/json" } : {}),
        ...options.headers,
      },
    });
    const payload = await response.json().catch(() => ({}));
    if (response.status === 401) {
      const next = `${window.location.pathname.split("/").pop() || "app.html"}${window.location.hash}`;
      window.location.replace(`login.html?next=${encodeURIComponent(next)}`);
      throw new Error("Authentication required");
    }
    if (!response.ok) {
      throw new Error(payload.error?.message || `API request failed (${response.status})`);
    }
    return payload.data;
  }

  async function loadProjectAssets() {
    projectAssets = await apiRequest(`/projects/${projectID}/assets`);
    hydrateSocialStudio();
    hydrateBlogAssets();
    return projectAssets;
  }

  async function uploadProjectAsset(file, purpose) {
    if (!file) throw new Error(currentLanguage === "hr" ? "Datoteka nije odabrana." : "No file selected.");
    if (file.size < 1 || file.size > 10 * 1024 * 1024) {
      throw new Error(currentLanguage === "hr" ? "Datoteka mora imati najviše 10 MB." : "The file must be no larger than 10 MB.");
    }
    const form = new FormData();
    form.append("file", file);
    form.append("purpose", purpose);
    const asset = await apiRequest(`/projects/${projectID}/assets`, { method: "POST", body: form });
    projectAssets = [asset, ...projectAssets.filter((item) => item.id !== asset.id)];
    return asset;
  }

  async function deleteProjectAsset(asset) {
    if (!asset?.id) return;
    await apiRequest(`/projects/${projectID}/assets/${asset.id}`, { method: "DELETE" });
    const objectURL = assetObjectURLs.get(asset.id);
    if (objectURL) URL.revokeObjectURL(objectURL);
    assetObjectURLs.delete(asset.id);
    projectAssets = projectAssets.filter((item) => item.id !== asset.id);
  }

  async function assetObjectURL(asset) {
    if (!asset?.id) return "";
    if (assetObjectURLs.has(asset.id)) return assetObjectURLs.get(asset.id);
    const response = await fetch(`${apiBase}/projects/${projectID}/assets/${asset.id}/download`, { credentials: "include" });
    if (!response.ok) {
      const payload = await response.json().catch(() => ({}));
      throw new Error(payload.error?.message || `Asset download failed (${response.status})`);
    }
    const objectURL = URL.createObjectURL(await response.blob());
    assetObjectURLs.set(asset.id, objectURL);
    return objectURL;
  }

  async function downloadAsset(asset) {
    try {
      const url = await assetObjectURL(asset);
      const link = document.createElement("a");
      link.href = url;
      link.download = asset.filename || "millena-asset";
      document.body.append(link);
      link.click();
      link.remove();
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Preuzimanje datoteke" : "Downloading file", error);
    }
  }

  function renderAssetChip(asset, onRemove = null) {
    const chip = document.createElement("span");
    chip.className = "asset-chip";
    const icon = document.createElement("i");
    icon.dataset.lucide = asset.mimeType?.startsWith("image/") ? "image" : asset.mimeType?.startsWith("video/") ? "video" : "file-text";
    const name = document.createElement("span");
    name.textContent = asset.filename;
    name.title = `${asset.filename} · ${Math.max(1, Math.round(asset.sizeBytes / 1024))} KB`;
    const download = document.createElement("button");
    download.type = "button";
    download.setAttribute("aria-label", currentLanguage === "hr" ? "Preuzmi datoteku" : "Download file");
    const downloadIcon = document.createElement("i");
    downloadIcon.dataset.lucide = "download";
    download.append(downloadIcon);
    download.addEventListener("click", () => downloadAsset(asset));
    chip.append(icon, name, download);
    if (onRemove) {
      const remove = document.createElement("button");
      remove.type = "button";
      remove.setAttribute("aria-label", currentLanguage === "hr" ? "Ukloni privitak" : "Remove attachment");
      const removeIcon = document.createElement("i");
      removeIcon.dataset.lucide = "x";
      remove.append(removeIcon);
      remove.addEventListener("click", () => onRemove(asset));
      chip.append(remove);
    }
    return chip;
  }

  function socialCard(provider) {
    return document.querySelector(`[data-social-provider="${provider}"]`);
  }

  function renderSocialConnections() {
    document.querySelectorAll("[data-social-provider]").forEach((card) => {
      const provider = card.dataset.socialProvider;
      const connection = socialConnections.get(provider);
      const account = card.querySelector("[data-social-account]");
      const connectButton = card.querySelector("[data-social-connect]");
      const footer = card.querySelector("[data-social-footer]");
      const status = card.querySelector("[data-social-status]");
      const health = card.querySelector("[data-social-health]");

      card.classList.toggle("connected", Boolean(connection));
      if (connectButton) connectButton.hidden = Boolean(connection);
      if (footer) footer.hidden = !connection;
      if (health) health.className = connection ? "health-ok" : "health-warn";

      if (connection) {
        const accountCopy = `${connection.displayName} · ${connection.accountHandle}`;
        if (account) {
          account.textContent = accountCopy;
          account.dataset.hr = accountCopy;
          account.dataset.en = accountCopy;
        }
        if (status) {
          status.textContent = currentLanguage === "hr" ? "Sandbox povezan" : "Sandbox connected";
          status.dataset.hr = "Sandbox povezan";
          status.dataset.en = "Sandbox connected";
        }
        card.dataset.connectionId = connection.id;
      } else {
        if (account) {
          account.dataset.hr = "Testni račun nije povezan.";
          account.dataset.en = "No test account connected.";
          account.textContent = account.dataset[currentLanguage];
        }
        delete card.dataset.connectionId;
      }
    });

    const channelButtons = [...document.querySelectorAll("[data-social-channel]")];
    channelButtons.forEach((button) => {
      button.classList.toggle("channel-connected", socialConnections.has(button.dataset.socialChannel));
    });
    const activeChannel = channelButtons.find((button) => button.classList.contains("active"));
    if (!activeChannel || !socialConnections.has(activeChannel.dataset.socialChannel)) {
      const fallback = channelButtons.find((button) => socialConnections.has(button.dataset.socialChannel)) || channelButtons[0];
      channelButtons.forEach((button) => button.classList.toggle("active", button === fallback));
      if (fallback?.dataset.socialChannel) activeSocialChannel = fallback.dataset.socialChannel;
    } else {
      activeSocialChannel = activeChannel.dataset.socialChannel;
    }
    renderSetupChannelStatuses();
    renderSocialAutomation();
    updateSocialQuality();
    applyRoleUI();
    refreshIcons();
  }

  function renderSocialHistory() {
    const list = document.querySelector("#social-history-list");
    const empty = document.querySelector("#social-history-empty");
    if (!list || !empty) return;

    list.replaceChildren();
    const rows = socialPosts
      .flatMap((post) => (post.publications || []).map((publication) => ({ post, publication })))
      .slice(0, 5);
    empty.hidden = rows.length > 0;

    rows.forEach(({ post, publication }) => {
      const item = document.createElement("li");
      const provider = document.createElement("strong");
      const excerpt = document.createElement("span");
      const status = document.createElement("small");
      provider.textContent = publication.provider;
      excerpt.textContent = post.body;
      const publicationLabels = {
        published: currentLanguage === "hr" ? "Objavljeno u sandboxu" : "Published in sandbox",
        scheduled: currentLanguage === "hr" ? "Zakazano u sandboxu" : "Scheduled in sandbox",
        failed: currentLanguage === "hr" ? "Sandbox objava nije uspjela" : "Sandbox publishing failed",
      };
      status.textContent = publicationLabels[publication.status]
        || (currentLanguage === "hr" ? "Obrada u sandboxu" : "Processing in sandbox");
      status.title = `${currentLanguage === "hr" ? "ID objave" : "Publication ID"}: ${publication.id}`;
      if (post.assets?.length) status.textContent += ` · ${post.assets.length} ${currentLanguage === "hr" ? "medija" : "media"}`;
      item.append(provider, excerpt, status);
      list.append(item);
    });
  }

  async function loadSocialConnections() {
    const connections = await apiRequest(`/projects/${projectID}/social/connections`);
    socialConnections = new Map(connections.map((connection) => [connection.provider, connection]));
    renderSocialConnections();
  }

  async function loadSocialPosts() {
    socialPosts = await apiRequest(`/projects/${projectID}/social/posts`);
    renderSocialHistory();
  }

  async function loadSocialData() {
    await Promise.all([loadSocialConnections(), loadSocialPosts()]);
    hydrateSocialStudio();
  }

  function selectedSocialChannel() {
    return activeSocialChannel || document.querySelector("[data-social-channel].active")?.dataset.socialChannel || "linkedin";
  }

  function latestContentOfKind(kind) {
    return [...contentItems]
      .filter((item) => item.kind === kind)
      .sort((left, right) => new Date(right.updatedAt) - new Date(left.updatedAt))[0] || null;
  }

  function renderSocialAutomation() {
    const channel = selectedSocialChannel();
    const rule = automationRules.find((candidate) => candidate.ruleKey === channel);
    const input = document.querySelector("#social-automation-enabled");
    if (input) {
      input.dataset.automationRuleKey = channel;
      input.checked = Boolean(rule?.enabled);
    }
    const label = document.querySelector("#social-rule-label");
    if (label) label.textContent = `${currentLanguage === "hr" ? "Pravilo za" : "Rule for"} ${channel}`;
  }

  function socialPlainText(value) {
    const source = String(value || "");
    if (!/<\/?[a-z][\s\S]*>/i.test(source)) return source.trim();
    const template = document.createElement("template");
    template.innerHTML = source;
    return (template.content.textContent || source).trim();
  }

  function socialVariantLocale() {
    const language = String(projectProfile?.primaryLanguage || currentLanguage || "hr").toLowerCase();
    return language === "en" ? "en" : "hr";
  }

  function socialVariantFor(channel = selectedSocialChannel(), variants = socialVariants) {
    const locale = socialVariantLocale();
    return variants.find((variant) => variant.channel === channel && variant.locale === locale)
      || variants.find((variant) => variant.channel === channel)
      || null;
  }

  function linkedSocialPublication(variant) {
    if (!variant?.id) return null;
    const post = socialPosts.find((candidate) => candidate.contentVariantId === variant.id);
    if (!post) return null;
    const publication = (post.publications || []).find((candidate) => candidate.provider === variant.channel)
      || post.publications?.[0]
      || null;
    return { post, publication };
  }

  function variantWithPublicationState(variant) {
    const linked = linkedSocialPublication(variant);
    if (!linked) return variant;
    const { post, publication } = linked;
    return {
      ...variant,
      metadata: {
        ...(variant.metadata || {}),
        lastKnownPublication: {
          channel: variant.channel,
          socialPostId: post.id,
          publicationId: publication?.id || null,
          status: publication?.status || post.status || variant.status,
          externalReference: publication?.externalReference || null,
          publishedAt: publication?.publishedAt || null,
          lastError: publication?.lastError || null,
        },
      },
    };
  }

  function socialStrategyAvailable() {
    return Boolean(projectStrategy?.revision || projectStrategy?.sixMonthGoal || projectStrategy?.sourceFilename || projectStrategy?.sourceText);
  }

  function renderSocialFactsStatus() {
    const status = document.querySelector("#social-facts-status");
    if (!status) return;
    const hasStrategy = socialStrategyAvailable();
    status.className = `status-pill ${hasStrategy ? "auto" : "review"}`;
    const icon = document.createElement("i");
    icon.dataset.lucide = hasStrategy ? "check" : "circle-alert";
    const label = document.createElement("span");
    label.textContent = hasStrategy
      ? (currentLanguage === "hr" ? `Iz strategije · rev ${projectStrategy.revision || 1}` : `From strategy · rev ${projectStrategy.revision || 1}`)
      : (currentLanguage === "hr" ? "Strategija nije dodana" : "Strategy is not added");
    status.replaceChildren(icon, label);
  }

  function renderSocialRecordSelector() {
    const select = document.querySelector("#social-content-select");
    if (!select) return;
    const socialItems = [...contentItems]
      .filter((item) => item.kind === "social")
      .sort((left, right) => new Date(right.updatedAt) - new Date(left.updatedAt));
    select.replaceChildren();
    const create = document.createElement("option");
    create.value = "__new__";
    create.textContent = currentLanguage === "hr" ? "+ Nova društvena skica" : "+ New social draft";
    select.append(create);
    socialItems.forEach((item) => {
      const option = document.createElement("option");
      option.value = item.id;
      option.textContent = `${item.title} · ${contentStatusLabel(item.status)}`;
      select.append(option);
    });
    select.value = socialNewRecordSelected ? "__new__" : (activeSocialContentID || "__new__");
    if (!select.value) select.value = "__new__";
  }

  function renderSocialVariantState(variant) {
    const status = document.querySelector("#social-save-state");
    if (!status || socialDraftDirty) return;
    const labels = {
      draft: currentLanguage === "hr" ? "Spremljeno" : "Saved",
      in_review: currentLanguage === "hr" ? "Čeka pregled" : "In review",
      approved: currentLanguage === "hr" ? "Odobreno" : "Approved",
      scheduled: currentLanguage === "hr" ? "Zakazano" : "Scheduled",
      published: currentLanguage === "hr" ? "Objavljeno" : "Published",
      failed: currentLanguage === "hr" ? "Objava nije uspjela" : "Publishing failed",
    };
    status.textContent = labels[variant?.status] || (currentLanguage === "hr" ? "Nova skica" : "New draft");
    const publication = variant?.metadata?.lastKnownPublication;
    status.title = publication?.publicationId
      ? `${currentLanguage === "hr" ? "ID objave" : "Publication ID"}: ${publication.publicationId}`
      : "";
  }

  async function resolveSocialSource(sourceID) {
    if (!sourceID) return latestContentOfKind("source");
    const cached = contentItems.find((item) => item.id === sourceID);
    if (cached) return cached;
    try {
      const source = await apiRequest(`/projects/${projectID}/content/items/${sourceID}`);
      if (source && !contentItems.some((item) => item.id === source.id)) contentItems.push(source);
      return source;
    } catch (error) {
      console.warn("Millena social source could not be reloaded", error);
      return latestContentOfKind("source");
    }
  }

  function updateSocialQuality() {
    const body = document.querySelector(".post-editor")?.innerText.trim() || "";
    const channel = selectedSocialChannel();
    const connectionReady = socialConnections.has(channel);
    const hasStrategy = socialStrategyAvailable();
    const hasSource = Boolean(activeSocialSourceID);
    const lengthReady = body.length >= 80 && body.length <= 3000;
    const hasMedia = socialMediaAssets.length > 0;
    const forbiddenTokens = String(projectStrategy?.forbiddenTopics || "")
      .split(/[,;\n]/).map((value) => value.trim().toLocaleLowerCase("hr")).filter((value) => value.length >= 4);
    const forbiddenClear = !forbiddenTokens.some((token) => body.toLocaleLowerCase("hr").includes(token));
    const checks = [
      [lengthReady, currentLanguage === "hr" ? `Duljina teksta ${body.length}/3.000` : `Copy length ${body.length}/3,000`],
      [hasStrategy, currentLanguage === "hr" ? "Aktivan strateški kontekst" : "Active strategy context"],
      [hasSource, currentLanguage === "hr" ? "Izvor je povezan" : "Source is linked"],
      [connectionReady, currentLanguage === "hr" ? `${channel} sandbox račun je povezan` : `${channel} sandbox account is connected`],
      [forbiddenClear, currentLanguage === "hr" ? "Nema podudaranja sa zabranjenim temama" : "No restricted-topic matches"],
      [hasMedia, currentLanguage === "hr" ? "Medij je dodan (opcionalno)" : "Media is attached (optional)"],
    ];
    const weights = [20, 20, 15, 20, 15, 10];
    const score = checks.reduce((sum, check, index) => sum + (check[0] ? weights[index] : 0), 0);
    setText("#social-character-count", `${body.length} / 3.000`);
    setText("#social-publish-score", `${score} / 100`);
    setText("#social-publish-readiness", score >= 80
      ? (currentLanguage === "hr" ? "Lokalna provjerna lista je spremna" : "Local checklist is ready")
      : (currentLanguage === "hr" ? "Dovršite označene lokalne provjere" : "Complete the highlighted local checks"));
    setText("#social-tone-status", hasStrategy
      ? (currentLanguage === "hr" ? `Strategija rev ${projectStrategy.revision || 1} uključena · činjenice potvrđuje urednik` : `Strategy rev ${projectStrategy.revision || 1} included · facts require editor review`)
      : (currentLanguage === "hr" ? "Dodajte strategiju; činjenice i ton potvrđuje urednik" : "Add a strategy; an editor must verify facts and tone"));
    const root = document.querySelector("#social-publish-checks");
    if (root) {
      root.replaceChildren();
      checks.forEach(([passed, copy]) => {
        const row = document.createElement("div");
        row.classList.toggle("check-missing", !passed);
        const icon = document.createElement("i");
        icon.dataset.lucide = passed ? "check" : "circle-alert";
        const label = document.createElement("span");
        label.textContent = copy;
        row.append(icon, label);
        root.append(row);
      });
    }
    refreshIcons();
  }

  function renderSocialSource(item) {
    activeSocialSourceID = item?.id || "";
    const author = item?.metadata?.author || sessionUser?.displayName || "Millena AI";
    const avatar = author.split(/\s+/).slice(0, 2).map((part) => part[0]).join("").toUpperCase() || "AI";
    setText("#social-source-avatar", avatar);
    setText("#social-source-author", author);
    setText("#social-source-meta", item
      ? `${contentSourceLabel(item.source)} · ${formatDateTime(item.updatedAt)}`
      : (currentLanguage === "hr" ? "Nema izvornog zapisa" : "No source entry"));
    setText("#social-source-body", socialPlainText(item?.body || item?.summary || (currentLanguage === "hr" ? "Dodajte zapis kategorije Izvorni materijal u bazi sadržaja." : "Add a Source material entry to the content database.")));
    const sourceAssets = (item?.metadata?.assetIds || []).map((id) => projectAssets.find((asset) => asset.id === id)).filter(Boolean);
    const sourceRoot = document.querySelector("#social-source-assets");
    if (sourceRoot) {
      sourceRoot.replaceChildren();
      sourceAssets.forEach((asset) => sourceRoot.append(renderAssetChip(asset)));
      sourceRoot.hidden = sourceAssets.length === 0;
    }
    const facts = document.querySelector("#social-facts");
    if (facts) {
      facts.replaceChildren();
      const values = [
        projectStrategy?.brandMessage && `${currentLanguage === "hr" ? "Poruka" : "Message"}: ${projectStrategy.brandMessage}`,
        projectStrategy?.proofPoints && `${currentLanguage === "hr" ? "Dokazi" : "Proof"}: ${projectStrategy.proofPoints}`,
        projectStrategy?.forbiddenTopics && `${currentLanguage === "hr" ? "Ne koristiti" : "Avoid"}: ${projectStrategy.forbiddenTopics}`,
        ...(projectStrategy?.priorityTopics || []).slice(0, 3).map((topic) => `${currentLanguage === "hr" ? "Tema" : "Topic"}: ${topic}`),
      ].filter(Boolean);
      (values.length ? values : [currentLanguage === "hr" ? "Strategija još nema dovoljno konteksta." : "The strategy does not have enough context yet."]).forEach((value) => {
        const itemNode = document.createElement("li");
        itemNode.textContent = value;
        facts.append(itemNode);
      });
    }
    renderSocialFactsStatus();
  }

  async function hydrateSocialStudio(force = false) {
    const token = ++socialHydrationToken;
    const draft = socialNewRecordSelected
      ? null
      : (contentItems.find((item) => item.id === activeSocialContentID) || latestContentOfKind("social"));
    if (draft) {
      activeSocialContentID = draft.id;
      socialNewRecordSelected = false;
    }
    const channel = selectedSocialChannel();
    const editorKey = `${draft?.id || "new"}:${channel}:${socialVariantLocale()}`;
    const editor = document.querySelector(".post-editor");
    const sameEditor = editor?.dataset.variantKey === editorKey;

    let variants = draft && (!sameEditor || force || !socialVariants.length)
      ? await apiRequest(`/projects/${projectID}/content/items/${draft.id}/variants`).catch((error) => {
        console.error("Millena social variants failed to load", error);
        return [];
      })
      : socialVariants;
    if (token !== socialHydrationToken) return;
    variants = variants.map(variantWithPublicationState);
    socialVariants = variants;
    const variant = socialVariantFor(channel, variants);
    const sourceID = variant?.metadata?.sourceItemId || draft?.metadata?.sourceItemId || "";
    const source = await resolveSocialSource(sourceID);
    if (token !== socialHydrationToken) return;

    socialMediaAssets = ((variant ? variant.metadata?.assetIds : draft?.metadata?.assetIds) || [])
      .map((id) => projectAssets.find((asset) => asset.id === id))
      .filter(Boolean);
    if (editor && (!sameEditor || force) && !socialDraftDirty) {
      editor.textContent = socialPlainText(variant?.body ?? draft?.body ?? source?.body ?? "");
      editor.dataset.contentId = draft?.id || "";
      editor.dataset.variantKey = editorKey;
      editor.dataset.channel = channel;
    }
    setText("#social-editor-title", draft?.title || source?.title || (currentLanguage === "hr" ? "Nova društvena objava" : "New social post"));
    renderSocialSource(source);
    renderSocialRecordSelector();
    renderSocialMediaAssets();
    renderSocialAutomation();
    renderSocialVariantState(variant);
    updateSocialQuality();
    if (variant?.status === "scheduled") scheduleSocialPublicationRefresh(draft?.id, variant);
  }

  function scheduleSocialDraftSave() {
    window.clearTimeout(socialDraftTimer);
    socialDraftDirty = true;
    socialEditVersion += 1;
    const status = document.querySelector("#social-save-state");
    if (status) status.textContent = currentLanguage === "hr" ? "Nespremljeno" : "Unsaved";
    socialDraftTimer = window.setTimeout(() => {
      socialDraftTimer = 0;
      saveSocialDraft(true);
    }, 800);
  }

  async function flushSocialDraftSave() {
    window.clearTimeout(socialDraftTimer);
    socialDraftTimer = 0;
    if (!socialDraftDirty) {
      await socialVariantSaveQueue;
      return true;
    }
    const body = document.querySelector(".post-editor")?.innerText.trim() || "";
    return Boolean(await saveSocialDraft(true));
  }

  async function saveSocialDraft(silent = false, options = {}) {
    const editor = document.querySelector(".post-editor");
    const body = editor?.innerText.trim() || "";
    // A completely cleared existing variant is a meaningful edit. Only skip
    // an empty brand-new record, where there is nothing in the database yet.
    if (body.length < 2 && !activeSocialContentID) return null;
    const contentID = activeSocialContentID;
    const channel = selectedSocialChannel();
    const locale = socialVariantLocale();
    const version = socialEditVersion;
    const sourceItemID = activeSocialSourceID;
    const assetIDs = socialMediaAssets.map((asset) => asset.id);
    const existingItem = contentItems.find((item) => item.id === contentID) || null;
    const existingVariant = socialVariantFor(channel);
    const title = existingItem?.title || document.querySelector("#social-editor-title")?.textContent.trim()
      || body.split(/\n/)[0].slice(0, 120)
      || (currentLanguage === "hr" ? "Društvena objava" : "Social post");
    const snapshot = {
      contentID, channel, locale, version, body,
      title: title.slice(0, 180),
      sourceItemID,
      assetIDs,
      source: existingItem?.source || "manual",
      itemMetadata: existingItem?.metadata || {},
      variantMetadata: existingVariant?.metadata || {},
      status: options.status || "draft",
      scheduledFor: options.scheduledFor || null,
      metadataPatch: options.metadataPatch || {},
    };

    const persist = async () => {
      const saveState = document.querySelector("#social-save-state");
      if (saveState && activeSocialContentID === snapshot.contentID && selectedSocialChannel() === snapshot.channel) {
        saveState.textContent = currentLanguage === "hr" ? "Spremanje…" : "Saving…";
      }
      try {
        let item = contentItems.find((candidate) => candidate.id === snapshot.contentID) || null;
        if (!item && !snapshot.contentID && activeSocialContentID) {
          item = contentItems.find((candidate) => candidate.id === activeSocialContentID) || null;
        }
        if (!item) {
          item = await apiRequest(`/projects/${projectID}/content/items`, {
            method: "POST",
            body: JSON.stringify({
              kind: "social", status: "draft", title: snapshot.title, summary: snapshot.body.slice(0, 500), body: snapshot.body,
              channels: [], scheduledFor: null, source: snapshot.source,
              metadata: {
                ...snapshot.itemMetadata,
                editor: "social-studio",
                sourceItemId: snapshot.sourceItemID || null,
                format: "plain_text",
              },
            }),
          });
          contentItems.unshift(item);
        }

        const currentVariant = socialVariants.find((candidate) => candidate.contentItemId === item.id
          && candidate.channel === snapshot.channel && candidate.locale === snapshot.locale);
        const saved = await apiRequest(`/projects/${projectID}/content/items/${item.id}/variants`, {
          method: "PUT",
          body: JSON.stringify({
            channel: snapshot.channel,
            locale: snapshot.locale,
            title: snapshot.title,
            summary: snapshot.body.slice(0, 500),
            body: snapshot.body,
            status: snapshot.status,
            scheduledFor: snapshot.status === "scheduled" ? snapshot.scheduledFor : null,
            metadata: {
              ...snapshot.variantMetadata,
              ...(currentVariant?.metadata || {}),
              editor: "social-studio",
              format: "plain_text",
              syncedFromItem: false,
              sourceItemId: snapshot.sourceItemID || null,
              assetIds: snapshot.assetIDs,
              ...snapshot.metadataPatch,
            },
          }),
        });

        activeSocialContentID = item.id;
        socialNewRecordSelected = false;
        const itemIndex = contentItems.findIndex((candidate) => candidate.id === item.id);
        const localItem = {
          ...contentItems[itemIndex],
          channels: [...new Set([...(contentItems[itemIndex]?.channels || []), saved.channel])],
          status: saved.status === "scheduled" ? "scheduled" : contentItems[itemIndex]?.status || "draft",
          scheduledFor: saved.status === "scheduled" ? saved.scheduledFor : contentItems[itemIndex]?.scheduledFor || null,
          updatedAt: saved.updatedAt,
        };
        if (itemIndex >= 0) contentItems[itemIndex] = localItem;
        else contentItems.unshift({ ...item, ...localItem });
        const variantIndex = socialVariants.findIndex((candidate) => candidate.id === saved.id);
        if (variantIndex >= 0) socialVariants[variantIndex] = variantWithPublicationState(saved);
        else socialVariants.push(variantWithPublicationState(saved));
        if (editor && selectedSocialChannel() === snapshot.channel) {
          editor.dataset.contentId = item.id;
          editor.dataset.variantKey = `${item.id}:${snapshot.channel}:${snapshot.locale}`;
        }
        if (activeSocialContentID === item.id && selectedSocialChannel() === snapshot.channel && socialEditVersion === snapshot.version) {
          socialDraftDirty = false;
          renderSocialVariantState(saved);
        }
        setText("#social-editor-title", item.title);
        renderSocialRecordSelector();
        renderContent();
        return saved;
      } catch (error) {
        if (saveState) saveState.textContent = currentLanguage === "hr" ? "Greška spremanja" : "Save failed";
        if (!silent) showDomainError(currentLanguage === "hr" ? "Social skica" : "Social draft", error);
        return null;
      }
    };

    const queued = socialVariantSaveQueue.then(persist, persist);
    socialVariantSaveQueue = queued.then(() => undefined, () => undefined);
    return queued;
  }

  function renderSocialMediaAssets() {
    const root = document.querySelector("#social-media-assets");
    if (!root) return;
    root.replaceChildren();
    socialMediaAssets.forEach((asset) => {
      const preview = document.createElement("span");
      preview.className = "asset-preview";
      const media = document.createElement(asset.mimeType?.startsWith("video/") ? "video" : "img");
      if (media.tagName === "VIDEO") media.muted = true;
      else media.alt = asset.filename;
      assetObjectURL(asset).then((url) => { media.src = url; }).catch((error) => console.error("Social media preview failed", error));
      preview.title = currentLanguage === "hr" ? `Otvori pregled: ${asset.filename}` : `Open preview: ${asset.filename}`;
      preview.setAttribute("role", "button");
      preview.tabIndex = 0;
      const openLabel = document.createElement("span");
      openLabel.className = "asset-preview-open";
      const openIcon = document.createElement("i");
      openIcon.dataset.lucide = "expand";
      const openText = document.createElement("span");
      openText.textContent = currentLanguage === "hr" ? "Pregled" : "Preview";
      openLabel.append(openIcon, openText);
      const openPreview = () => openSocialMediaPreview(asset);
      preview.addEventListener("click", (event) => { if (!event.target.closest("button")) openPreview(); });
      preview.addEventListener("keydown", (event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); openPreview(); } });
      const remove = document.createElement("button");
      remove.type = "button";
      remove.setAttribute("aria-label", currentLanguage === "hr" ? "Ukloni medij" : "Remove media");
      const icon = document.createElement("i");
      icon.dataset.lucide = "x";
      remove.append(icon);
      remove.addEventListener("click", () => removeSocialMediaAsset(asset));
      preview.append(media, openLabel, remove);
      root.append(preview);
    });
    updateSocialQuality();
    refreshIcons();
  }

  async function openSocialMediaPreview(asset) {
    const stage = document.querySelector("#media-preview-stage");
    if (!stage || !asset) return;
    stage.replaceChildren();
    setText("#media-preview-title", asset.filename || (currentLanguage === "hr" ? "Pregled medija" : "Media preview"));
    const media = document.createElement(asset.mimeType?.startsWith("video/") ? "video" : "img");
    if (media.tagName === "VIDEO") media.controls = true;
    else media.alt = asset.filename || "";
    stage.append(media);
    openDomainModal("media-preview-modal");
    try {
      media.src = await assetObjectURL(asset);
    } catch (error) {
      closeDomainModal("media-preview-modal");
      showDomainError(currentLanguage === "hr" ? "Pregled medija" : "Media preview", error);
    }
  }

  function closeSocialMediaPreview() {
    const stage = document.querySelector("#media-preview-stage");
    stage?.replaceChildren();
    closeDomainModal("media-preview-modal");
  }

  async function removeSocialMediaAsset(asset) {
    socialMediaAssets = socialMediaAssets.filter((item) => item.id !== asset.id);
    const usedByPost = socialPosts.some((post) => post.assets?.some((item) => item.id === asset.id));
    const usedByAnotherVariant = socialVariants.some((variant) => variant.channel !== selectedSocialChannel()
      && variant.metadata?.assetIds?.includes(asset.id));
    const usedByAnotherItem = contentItems.some((item) => item.id !== activeSocialContentID && item.metadata?.assetIds?.includes(asset.id));
    try {
      if (!usedByPost && !usedByAnotherVariant && !usedByAnotherItem) await deleteProjectAsset(asset);
      renderSocialMediaAssets();
      scheduleSocialDraftSave();
      await flushSocialDraftSave();
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Brisanje medija" : "Deleting media", error);
    }
  }

  async function uploadSocialMedia(input) {
    const files = [...(input.files || [])];
    input.value = "";
    if (!files.length) return;
    if (socialMediaAssets.length + files.length > 10) {
      openActionModal(currentLanguage === "hr" ? "Najviše deset medija po objavi" : "Up to ten media files per post", "", currentLanguage === "hr" ? "Previše datoteka" : "Too many files");
      return;
    }
    const button = document.querySelector("#social-media-add");
    if (button) button.disabled = true;
    try {
      for (const file of files) {
        socialMediaAssets.push(await uploadProjectAsset(file, "social_media"));
        renderSocialMediaAssets();
      }
      scheduleSocialDraftSave();
      await flushSocialDraftSave();
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Upload medija" : "Media upload", error);
    } finally {
      if (button) button.disabled = false;
    }
  }

  function openSocialModal(provider) {
    const modal = document.querySelector("#social-connect-modal");
    if (!modal) return;
    pendingSocialProvider = provider;
    const providerName = socialCard(provider)?.querySelector("h3")?.textContent || provider;
    const titleProvider = modal.querySelector("#social-provider-name");
    const displayName = modal.querySelector("#social-display-name");
    const accountHandle = modal.querySelector("#social-account-handle");
    if (titleProvider) titleProvider.textContent = providerName;
    if (displayName) displayName.value = "";
    if (accountHandle) accountHandle.value = "";
    modal.classList.add("open");
    modal.setAttribute("aria-hidden", "false");
    document.body.style.overflow = "hidden";
    displayName?.focus();
  }

  function closeSocialModal() {
    const modal = document.querySelector("#social-connect-modal");
    if (!modal) return;
    modal.classList.remove("open");
    modal.setAttribute("aria-hidden", "true");
    document.body.style.overflow = "";
    pendingSocialProvider = "";
  }

  async function connectSocialAccount() {
    const displayName = document.querySelector("#social-display-name");
    const accountHandle = document.querySelector("#social-account-handle");
    const submit = document.querySelector("#social-connect-submit");
    if (!pendingSocialProvider || !displayName || !accountHandle || !submit) return;

    if (displayName.value.trim().length < 2) {
      displayName.setCustomValidity(currentLanguage === "hr" ? "Unesite naziv računa." : "Enter an account name.");
      displayName.reportValidity();
      displayName.setCustomValidity("");
      return;
    }
    if (accountHandle.value.trim().length < 2) {
      accountHandle.setCustomValidity(currentLanguage === "hr" ? "Unesite korisničko ime ili oznaku računa." : "Enter a username or account handle.");
      accountHandle.reportValidity();
      accountHandle.setCustomValidity("");
      return;
    }

    submit.disabled = true;
    try {
      const connection = await apiRequest(`/projects/${projectID}/social/connections`, {
        method: "POST",
        body: JSON.stringify({
          provider: pendingSocialProvider,
          displayName: displayName.value.trim(),
          accountHandle: accountHandle.value.trim(),
          mode: "sandbox",
        }),
      });
      socialConnections.set(connection.provider, connection);
      closeSocialModal();
      renderSocialConnections();
      showToast("socialConnected");
    } catch (error) {
      console.error("Millena social connection failed", error);
      showToast("socialError");
    } finally {
      submit.disabled = false;
    }
  }

  async function testSocialConnection(provider) {
    const connection = socialConnections.get(provider);
    if (!connection) return;
    try {
      const updated = await apiRequest(`/projects/${projectID}/social/connections/${connection.id}/test`, { method: "POST" });
      socialConnections.set(provider, updated);
      renderSocialConnections();
      showToast("socialTested");
    } catch (error) {
      console.error("Millena social test failed", error);
      showToast("socialError");
    }
  }

  async function disconnectSocialAccount(provider) {
    const connection = socialConnections.get(provider);
    if (!connection) return;
    const label = connection.displayName || connection.accountHandle || provider;
    const confirmed = window.confirm(currentLanguage === "hr"
      ? `Odspojiti sandbox račun „${label}”?`
      : `Disconnect sandbox account “${label}”?`);
    if (!confirmed) return;
    try {
      await apiRequest(`/projects/${projectID}/social/connections/${connection.id}`, { method: "DELETE" });
      socialConnections.delete(provider);
      renderSocialConnections();
      showToast("socialDisconnected");
    } catch (error) {
      console.error("Millena social disconnect failed", error);
      showToast("socialError");
    }
  }

  async function refreshSocialPublicationState(contentID, variantID) {
    const [variants, posts, item] = await Promise.all([
      apiRequest(`/projects/${projectID}/content/items/${contentID}/variants`),
      apiRequest(`/projects/${projectID}/social/posts`),
      apiRequest(`/projects/${projectID}/content/items/${contentID}`),
    ]);
    socialPosts = posts;
    const enriched = variants.map(variantWithPublicationState);
    const itemIndex = contentItems.findIndex((candidate) => candidate.id === item.id);
    if (itemIndex >= 0) contentItems[itemIndex] = item;
    else contentItems.unshift(item);
    if (activeSocialContentID === contentID) socialVariants = enriched;
    const variant = enriched.find((candidate) => candidate.id === variantID) || null;
    renderSocialHistory();
    renderSocialRecordSelector();
    renderContent();
    if (activeSocialContentID === contentID && variant?.channel === selectedSocialChannel()) {
      renderSocialVariantState(variant);
      updateSocialQuality();
    }
    return variant;
  }

  async function pollSocialPublication(contentID, variant, attempts = 10) {
    if (!contentID || !variant?.id) return variant;
    const pendingTimer = socialPublicationTimers.get(variant.id);
    if (pendingTimer) window.clearTimeout(pendingTimer);
    socialPublicationTimers.delete(variant.id);
    let current = variant;
    for (let attempt = 0; attempt < attempts; attempt += 1) {
      if (attempt > 0) await new Promise((resolve) => window.setTimeout(resolve, 750));
      try {
        current = await refreshSocialPublicationState(contentID, variant.id) || current;
      } catch (error) {
        console.warn("Millena social publication refresh failed", error);
      }
      if (current?.status && current.status !== "scheduled") return current;
    }
    if (current?.status === "scheduled") scheduleSocialPublicationRefresh(contentID, current);
    return current;
  }

  function scheduleSocialPublicationRefresh(contentID, variant) {
    if (!contentID || !variant?.id || variant.status !== "scheduled" || !variant.scheduledFor) return;
    const existing = socialPublicationTimers.get(variant.id);
    if (existing) window.clearTimeout(existing);
    const untilDue = new Date(variant.scheduledFor).getTime() - Date.now();
    const delay = untilDue > 0 ? untilDue + 2000 : 15000;
    const timer = window.setTimeout(() => {
      socialPublicationTimers.delete(variant.id);
      pollSocialPublication(contentID, variant).catch((error) => console.warn("Millena scheduled publication refresh failed", error));
    }, Math.max(500, delay));
    socialPublicationTimers.set(variant.id, timer);
  }

  async function publishSocialPost() {
    const selectedChannel = selectedSocialChannel();
    const connection = socialConnections.get(selectedChannel);
    if (!connection) {
      showToast("socialRequired");
      navigateTo("channels");
      return;
    }

    const editor = document.querySelector(".post-editor");
    const body = editor?.innerText.trim() || "";
    if (!body) {
      showToast("socialError");
      return;
    }
    if ([document.querySelector(".rewrite-button"), document.querySelector("#social-media-add")].some((button) => button?.disabled)) {
      showToast("socialError");
      return;
    }

    const schedule = document.querySelector("#social-publish-time")?.value;
    const scheduledFor = schedule === "five-minutes"
      ? new Date(Date.now() + 5 * 60 * 1000).toISOString()
      : new Date(Date.now() + 750).toISOString();
    const buttons = [...document.querySelectorAll("[data-social-publish]")];
    const channelButtons = [...document.querySelectorAll("[data-social-channel]")];
    const editorActionButtons = [...document.querySelectorAll(".editor-toolbar button, #social-media-add")];
    const editorActionDisabled = new Map(editorActionButtons.map((button) => [button, button.disabled]));
    const recordSelect = document.querySelector("#social-content-select");
    buttons.forEach((button) => { button.disabled = true; });
    channelButtons.forEach((button) => { button.disabled = true; });
    editorActionButtons.forEach((button) => { button.disabled = true; });
    if (recordSelect) recordSelect.disabled = true;
    if (editor) editor.contentEditable = "false";
    try {
      const flushed = await flushSocialDraftSave();
      if (!flushed) throw new Error(currentLanguage === "hr" ? "Skica se nije mogla spremiti." : "The draft could not be saved.");
      const requestedAt = new Date().toISOString();
      const savedVariant = await saveSocialDraft(true, {
        status: "scheduled",
        scheduledFor,
        metadataPatch: {
          publicationRequestedAt: requestedAt,
          lastKnownPublication: {
            channel: selectedChannel,
            status: "scheduled",
            scheduledFor,
            socialPostId: null,
            publicationId: null,
          },
        },
      });
      if (!savedVariant) throw new Error(currentLanguage === "hr" ? "Objava se nije mogla zakazati." : "The post could not be scheduled.");
      const contentID = savedVariant.contentItemId || activeSocialContentID;
      let publishedVariant = savedVariant;
      if (schedule === "five-minutes") {
        scheduleSocialPublicationRefresh(contentID, savedVariant);
      } else {
        publishedVariant = await pollSocialPublication(contentID, savedVariant);
      }
      if (publishedVariant?.status === "failed") {
        const failure = publishedVariant.metadata?.lastKnownPublication?.lastError;
        throw new Error(failure || (currentLanguage === "hr" ? "Sandbox objava nije uspjela." : "Sandbox publishing failed."));
      }
      loadCalendar().catch((error) => console.warn("Millena calendar refresh after social publish failed", error));
      updateSocialQuality();
      showToast(schedule === "five-minutes" ? "scheduled" : "socialPublished");
    } catch (error) {
      console.error("Millena social publish failed", error);
      showToast("socialError");
    } finally {
      buttons.forEach((button) => { button.disabled = false; });
      channelButtons.forEach((button) => { button.disabled = false; });
      editorActionButtons.forEach((button) => { button.disabled = editorActionDisabled.get(button); });
      if (recordSelect) recordSelect.disabled = false;
      if (editor) editor.contentEditable = "true";
    }
  }

  function startOfWeek(value) {
    const date = new Date(value);
    date.setHours(0, 0, 0, 0);
    const mondayOffset = (date.getDay() + 6) % 7;
    date.setDate(date.getDate() - mondayOffset);
    return date;
  }

  function addDays(value, days) {
    const date = new Date(value);
    date.setDate(date.getDate() + days);
    return date;
  }

  function calendarRange() {
    if (calendarView === "month") {
      const first = new Date(calendarCursor.getFullYear(), calendarCursor.getMonth(), 1);
      const from = startOfWeek(first);
      return { from, to: addDays(from, 42) };
    }
    const from = startOfWeek(calendarCursor);
    return { from, to: addDays(from, 7) };
  }

  function sameLocalDay(left, right) {
    return left.getFullYear() === right.getFullYear()
      && left.getMonth() === right.getMonth()
      && left.getDate() === right.getDate();
  }

  function calendarStatusClass(item) {
    if (item.status === "in_review") return "review";
    if (item.status === "suggestion") return "suggestion";
    if (item.status === "published") return "published";
    if (item.channel === "newsletter") return "newsletter";
    return "scheduled";
  }

  function calendarChannelIcon(channel) {
    return {
      blog: "globe-2", website: "globe-2", newsletter: "mail", x: "at-sign", pinterest: "pin",
      threads: "at-sign", reddit: "message-square", whatsapp: "message-circle",
      telegram: "send", webhook: "webhook", custom_api: "code-2", linkedin: "linkedin",
      instagram: "instagram", facebook: "facebook", youtube: "youtube", media: "newspaper",
    }[channel] || "workflow";
  }

  function createCalendarButton(item) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `cal-item ${calendarStatusClass(item)}`;
    button.dataset.calendarItem = item.id;
    const detailCopy = currentLanguage === "hr" ? "Otvori detalje" : "Open details";
    button.title = detailCopy;
    button.setAttribute("aria-label", `${item.title} · ${detailCopy}`);
    const channel = document.createElement("span");
    const icon = document.createElement("i");
    icon.dataset.lucide = calendarChannelIcon(item.channel);
    channel.append(icon, document.createTextNode(` ${item.channel}`));
    const title = document.createElement("strong");
    title.textContent = item.title;
    const time = document.createElement("small");
    time.textContent = new Intl.DateTimeFormat(currentLanguage === "hr" ? "hr-HR" : "en-GB", { hour: "2-digit", minute: "2-digit" }).format(new Date(item.scheduledFor));
    const action = document.createElement("em");
    action.className = "cal-item-action";
    action.textContent = currentLanguage === "hr" ? "Detalji →" : "Details →";
    button.append(channel, title, time, action);
    return button;
  }

  function updateCalendarLabel() {
    const label = document.querySelector("#calendar-range-label");
    if (!label) return;
    const locale = currentLanguage === "hr" ? "hr-HR" : "en-GB";
    if (calendarView === "month") {
      label.textContent = new Intl.DateTimeFormat(locale, { month: "long", year: "numeric" }).format(calendarCursor);
      return;
    }
    const from = startOfWeek(calendarCursor);
    const to = addDays(from, 6);
    label.textContent = `${new Intl.DateTimeFormat(locale, { day: "numeric", month: "short" }).format(from)} – ${new Intl.DateTimeFormat(locale, { day: "numeric", month: "short", year: "numeric" }).format(to)}`;
  }

  function renderWeekCalendar(grid) {
    grid.className = "week-grid";
    grid.replaceChildren();
    const monday = startOfWeek(calendarCursor);
    const locale = currentLanguage === "hr" ? "hr-HR" : "en-GB";
    const today = new Date();
    const corner = document.createElement("div");
    corner.className = "week-time";
    grid.append(corner);
    for (let dayIndex = 0; dayIndex < 7; dayIndex += 1) {
      const day = addDays(monday, dayIndex);
      const header = document.createElement("div");
      header.className = `week-day${sameLocalDay(day, today) ? " today" : ""}`;
      const weekday = document.createElement("small");
      weekday.textContent = new Intl.DateTimeFormat(locale, { weekday: "short" }).format(day).toUpperCase();
      const number = document.createElement("strong");
      number.textContent = String(day.getDate());
      header.append(weekday, number);
      grid.append(header);
    }

    [9, 12, 16].forEach((hour) => {
      const time = document.createElement("div");
      time.className = "week-time";
      time.textContent = `${String(hour).padStart(2, "0")}:00`;
      grid.append(time);
      for (let dayIndex = 0; dayIndex < 7; dayIndex += 1) {
        const day = addDays(monday, dayIndex);
        const cell = document.createElement("div");
        cell.className = "calendar-cell";
        const items = calendarItems.filter((item) => {
          const scheduled = new Date(item.scheduledFor);
          const bucket = scheduled.getHours() < 11 ? 9 : (scheduled.getHours() < 15 ? 12 : 16);
          return sameLocalDay(scheduled, day) && bucket === hour;
        });
        if (items.length) {
          items.forEach((item) => cell.append(createCalendarButton(item)));
        } else {
          const empty = document.createElement("button");
          empty.type = "button";
          empty.className = "calendar-empty";
          empty.dataset.calendarSlot = new Date(day.getFullYear(), day.getMonth(), day.getDate(), hour, 0).toISOString();
          empty.textContent = currentLanguage === "hr" ? "+ Dodaj" : "+ Add";
          cell.append(empty);
        }
        grid.append(cell);
      }
    });
  }

  function renderMonthCalendar(grid) {
    grid.className = "month-grid";
    grid.replaceChildren();
    const locale = currentLanguage === "hr" ? "hr-HR" : "en-GB";
    const range = calendarRange();
    const today = new Date();
    for (let dayIndex = 0; dayIndex < 7; dayIndex += 1) {
      const heading = document.createElement("div");
      heading.className = "month-day-head";
      heading.textContent = new Intl.DateTimeFormat(locale, { weekday: "short" }).format(addDays(range.from, dayIndex)).toUpperCase();
      grid.append(heading);
    }
    for (let dayIndex = 0; dayIndex < 42; dayIndex += 1) {
      const day = addDays(range.from, dayIndex);
      const cell = document.createElement("div");
      cell.className = `month-day${day.getMonth() !== calendarCursor.getMonth() ? " outside" : ""}${sameLocalDay(day, today) ? " today" : ""}`;
      const number = document.createElement("strong");
      number.textContent = String(day.getDate());
      cell.append(number);
      calendarItems.filter((item) => sameLocalDay(new Date(item.scheduledFor), day)).forEach((item) => cell.append(createCalendarButton(item)));
      const add = document.createElement("button");
      add.type = "button";
      add.className = "calendar-empty";
      add.dataset.calendarSlot = new Date(day.getFullYear(), day.getMonth(), day.getDate(), 9, 0).toISOString();
      add.textContent = "+";
      cell.append(add);
      grid.append(cell);
    }
  }

  function renderCalendar() {
    const grid = document.querySelector("#calendar-grid");
    if (!grid) return;
    updateCalendarLabel();
    if (calendarView === "month") renderMonthCalendar(grid);
    else renderWeekCalendar(grid);
    refreshIcons();
  }

  async function loadCalendar() {
    const requestToken = ++calendarLoadToken;
    const range = calendarRange();
    let items;
    try {
      items = await apiRequest(`/projects/${projectID}/calendar?from=${encodeURIComponent(range.from.toISOString())}&to=${encodeURIComponent(range.to.toISOString())}`);
    } catch (error) {
      if (requestToken !== calendarLoadToken) return null;
      throw error;
    }
    if (requestToken !== calendarLoadToken) return null;
    calendarItems = items;
    renderCalendar();
    return items;
  }

  function localDateTimeValue(value) {
    const date = new Date(value);
    const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000);
    return local.toISOString().slice(0, 16);
  }

  function openCalendarModal(item = null, scheduledFor = null) {
    const modal = document.querySelector("#calendar-modal");
    if (!modal) return;
    const fallback = scheduledFor ? new Date(scheduledFor) : new Date(Date.now() + 60 * 60 * 1000);
    document.querySelector("#calendar-item-id").value = item?.id || "";
    document.querySelector("#calendar-item-title").value = item?.title || "";
    document.querySelector("#calendar-item-summary").value = item?.summary || "";
    const channel = document.querySelector("#calendar-item-channel");
    channel.value = item?.channel || "linkedin";
    channel.disabled = Boolean(item?.contentVariantId);
    channel.title = item?.contentVariantId
      ? (currentLanguage === "hr" ? "Kanal je vezan uz sadržajnu varijantu i mijenja se u Social studiju." : "This channel belongs to a content variant; change it in the Social studio.")
      : "";
    document.querySelector("#calendar-item-status").value = item?.status || "draft";
    document.querySelector("#calendar-item-scheduled").value = localDateTimeValue(item?.scheduledFor || fallback);
    document.querySelector("#calendar-delete").hidden = !item;
    updateCalendarDetailPreview();
    modal.classList.add("open");
    modal.setAttribute("aria-hidden", "false");
    document.body.style.overflow = "hidden";
    document.querySelector("#calendar-item-title")?.focus();
  }

  function updateCalendarDetailPreview() {
    const channel = document.querySelector("#calendar-item-channel")?.value || "calendar";
    const status = document.querySelector("#calendar-item-status")?.value || "draft";
    const title = document.querySelector("#calendar-item-title")?.value.trim() || (currentLanguage === "hr" ? "Nova stavka" : "New item");
    const selectedChannel = document.querySelector("#calendar-item-channel option:checked")?.textContent || channel;
    const previewIcon = document.querySelector("#calendar-detail-icon");
    if (previewIcon) previewIcon.dataset.lucide = calendarChannelIcon(channel);
    const previewTitle = document.querySelector("#calendar-detail-preview-title");
    if (previewTitle) previewTitle.textContent = title;
    const previewMeta = document.querySelector("#calendar-detail-preview-meta");
    if (previewMeta) previewMeta.textContent = `${selectedChannel} · ${contentStatusLabel(status)}`;
    refreshIcons();
  }

  function closeCalendarModal() {
    const modal = document.querySelector("#calendar-modal");
    if (!modal) return;
    modal.classList.remove("open");
    modal.setAttribute("aria-hidden", "true");
    const channel = document.querySelector("#calendar-item-channel");
    if (channel) {
      channel.disabled = false;
      channel.title = "";
    }
    document.body.style.overflow = "";
  }

  async function saveCalendarItem() {
    const id = document.querySelector("#calendar-item-id")?.value;
    const titleInput = document.querySelector("#calendar-item-title");
    const scheduledInput = document.querySelector("#calendar-item-scheduled");
    if (!titleInput?.value.trim()) {
      titleInput?.reportValidity();
      return;
    }
    if (!scheduledInput?.value) {
      scheduledInput?.reportValidity();
      return;
    }
    const existing = calendarItems.find((item) => item.id === id) || null;
    const body = {
      title: titleInput.value.trim(),
      summary: document.querySelector("#calendar-item-summary")?.value.trim() || "",
      channel: document.querySelector("#calendar-item-channel")?.value,
      status: document.querySelector("#calendar-item-status")?.value,
      scheduledFor: new Date(scheduledInput.value).toISOString(),
      metadata: {
        ...(existing?.metadata || {}),
        source: existing?.metadata?.source || "calendar-ui",
        lastEditor: "calendar-ui",
        plan: projectAccess?.entitlement?.planCode || "unknown",
      },
    };
    const save = document.querySelector("#calendar-save");
    if (save) save.disabled = true;
    try {
      await apiRequest(id ? `/projects/${projectID}/calendar/items/${id}` : `/projects/${projectID}/calendar/items`, {
        method: id ? "PUT" : "POST",
        body: JSON.stringify(body),
      });
      closeCalendarModal();
      await loadCalendar();
      showToast("calendarSaved");
    } catch (error) {
      console.error("Millena calendar save failed", error);
      showToast("calendarError");
    } finally {
      if (save) save.disabled = false;
    }
  }

  async function deleteCalendarItem() {
    const id = document.querySelector("#calendar-item-id")?.value;
    if (!id) return;
    const item = calendarItems.find((candidate) => candidate.id === id);
    const title = item?.title || document.querySelector("#calendar-item-title")?.value.trim() || (currentLanguage === "hr" ? "stavku" : "item");
    const confirmed = window.confirm(currentLanguage === "hr"
      ? `Obrisati kalendarsku stavku „${title}”?`
      : `Delete calendar item “${title}”?`);
    if (!confirmed) return;
    try {
      await apiRequest(`/projects/${projectID}/calendar/items/${id}`, { method: "DELETE" });
      closeCalendarModal();
      await loadCalendar();
      showToast("calendarDeleted");
    } catch (error) {
      console.error("Millena calendar delete failed", error);
      showToast("calendarError");
    }
  }

  function contentKindLabel(kind) {
    const labels = {
      hr: { source: "Izvorni materijal", social: "Društvena objava", blog: "Blog članak", newsletter: "Newsletter", press_release: "Priopćenje", case_study: "Studija slučaja", event: "Događaj" },
      en: { source: "Source material", social: "Social post", blog: "Blog article", newsletter: "Newsletter", press_release: "Press release", case_study: "Case study", event: "Event" },
    };
    return labels[currentLanguage]?.[kind] || labels.hr[kind] || kind;
  }

  function contentKindIcon(kind) {
    return { source: "inbox", social: "messages-square", blog: "file-text", newsletter: "mail", press_release: "newspaper", case_study: "chart-no-axes-combined", event: "calendar-days" }[kind] || "file";
  }

  function defaultContentChannels(kind) {
    return {
      social: [activeSocialChannel || "linkedin"],
      blog: ["website"],
      newsletter: ["newsletter"],
      press_release: ["website", "linkedin"],
      case_study: ["website", "linkedin"],
      event: ["linkedin", "facebook"],
      source: [],
    }[kind] || [];
  }

  function aiGeneratedSummary(title, body) {
    const normalizedTitle = title.trim().toLocaleLowerCase();
    const paragraphs = body.split(/\n\s*\n/)
      .map((value) => value.replace(/^#+\s*/, "").replace(/\s+/g, " ").trim())
      .filter(Boolean);
    const candidate = paragraphs.find((value) => {
      const normalized = value.toLocaleLowerCase();
      return normalized !== normalizedTitle && !/^(predmet|subject):/i.test(value);
    }) || paragraphs[0] || "";
    return candidate.slice(0, 500);
  }

  function contentStatusLabel(status) {
    const labels = {
      hr: { draft: "Skica", in_review: "Za pregled", approved: "Odobreno", scheduled: "Zakazano", published: "Objavljeno", failed: "Greška" },
      en: { draft: "Draft", in_review: "In review", approved: "Approved", scheduled: "Scheduled", published: "Published", failed: "Failed" },
    };
    return labels[currentLanguage]?.[status] || labels.hr[status] || status;
  }

  function contentStatusClass(status) {
    return { draft: "working", in_review: "review", approved: "auto", scheduled: "ready", published: "auto", failed: "danger" }[status] || "working";
  }

  function contentSourceLabel(source) {
    const labels = {
      hr: { manual: "Ručni unos", ai: "AI + strategija", bot: "Millena bot", import: "Uvezeno" },
      en: { manual: "Manual", ai: "AI + strategy", bot: "Millena bot", import: "Imported" },
    };
    return labels[currentLanguage]?.[source] || labels.hr[source] || source;
  }

  function contentTiming(item) {
    const locale = currentLanguage === "hr" ? "hr-HR" : "en-GB";
    const value = item.scheduledFor || item.updatedAt;
    const formatted = new Intl.DateTimeFormat(locale, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
    if (item.scheduledFor) return formatted;
    return `${currentLanguage === "hr" ? "Izmjena" : "Updated"} · ${formatted}`;
  }

  function contentReviewLabel(item) {
    const reviewer = item?.metadata?.reviewedByName;
    if (!reviewer) return "";
    const reviewedAt = item.metadata?.reviewedAt ? ` · ${formatDateTime(item.metadata.reviewedAt)}` : "";
    if (item.metadata?.reviewDecision === "revision_requested") {
      const comment = item.metadata?.reviewComment ? ` — ${item.metadata.reviewComment}` : "";
      return currentLanguage === "hr" ? `Vraćeno u izradu: ${reviewer}${reviewedAt}${comment}` : `Returned for revision by ${reviewer}${reviewedAt}${comment}`;
    }
    return currentLanguage === "hr" ? `Pregledao/la ${reviewer}${reviewedAt}` : `Reviewed by ${reviewer}${reviewedAt}`;
  }

  function createContentRow(item) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "table-row content-record-row";
    row.dataset.contentItem = item.id;
    row.setAttribute("aria-label", `${item.title} · ${currentLanguage === "hr" ? "Otvori i uredi" : "Open and edit"}`);

    const titleWrap = document.createElement("span");
    titleWrap.className = "table-title";
    const thumb = document.createElement("span");
    thumb.className = `doc-thumb ${item.kind}`;
    const thumbIcon = document.createElement("i");
    thumbIcon.dataset.lucide = contentKindIcon(item.kind);
    thumb.append(thumbIcon);
    const titleCopy = document.createElement("span");
    const title = document.createElement("strong");
    title.textContent = item.title;
    const summary = document.createElement("small");
    const reviewLabel = contentReviewLabel(item);
    summary.textContent = [item.summary || item.body.slice(0, 100) || (currentLanguage === "hr" ? "Bez sažetka" : "No summary"), contentSourceLabel(item.source), reviewLabel].filter(Boolean).join(" · ");
    titleCopy.append(title, summary);
    titleWrap.append(thumb, titleCopy);

    const kind = document.createElement("span");
    kind.className = "content-kind-badge";
    kind.textContent = contentKindLabel(item.kind);

    const channels = document.createElement("span");
    channels.className = "channel-icons";
    if (item.channels?.length) {
      item.channels.slice(0, 4).forEach((channel) => {
        const icon = document.createElement("i");
        icon.dataset.lucide = calendarChannelIcon(channel === "media" ? "newspaper" : channel);
        icon.title = channel;
        channels.append(icon);
      });
    } else {
      channels.textContent = "—";
    }

    const timing = document.createElement("span");
    timing.textContent = contentTiming(item);
    const status = document.createElement("span");
    status.className = `status-pill ${contentStatusClass(item.status)}`;
    status.textContent = contentStatusLabel(item.status);
    const edit = document.createElement("i");
    edit.dataset.lucide = "pencil";
    row.append(titleWrap, kind, channels, timing, status, edit);
    return row;
  }

  function filteredContentItems() {
    const query = contentSearch.toLocaleLowerCase(currentLanguage === "hr" ? "hr" : "en");
    return contentItems.filter((item) => {
      if (contentKind !== "all" && item.kind !== contentKind) return false;
      if (contentStatus && item.status !== contentStatus) return false;
      if (!query) return true;
      return `${item.title} ${item.summary} ${item.body} ${(item.channels || []).join(" ")}`.toLocaleLowerCase(currentLanguage === "hr" ? "hr" : "en").includes(query);
    });
  }

  function renderContent() {
    const navigationCount = document.querySelector("#nav-content-count") || document.querySelector('.nav-item[data-screen-target="content"] b');
    if (navigationCount) navigationCount.textContent = String(contentItems.length);
    document.querySelectorAll("[data-content-count]").forEach((node) => {
      const kind = node.dataset.contentCount;
      node.textContent = String(kind === "all" ? contentItems.length : contentItems.filter((item) => item.kind === kind).length);
    });
    document.querySelectorAll("[data-content-kind]").forEach((button) => {
      const active = button.dataset.contentKind === contentKind;
      button.classList.toggle("active", active);
      button.setAttribute("aria-selected", String(active));
    });
    const items = filteredContentItems();
    const count = document.querySelector("#content-result-count");
    if (count) count.textContent = currentLanguage === "hr" ? `${items.length} zapisa` : `${items.length} entries`;
    const list = document.querySelector("#content-list");
    if (!list) return;
    list.replaceChildren();
    if (!items.length) {
      const empty = document.createElement("div");
      empty.className = "content-empty";
      const icon = document.createElement("i");
      icon.dataset.lucide = "search-x";
      const label = document.createElement("span");
      label.textContent = currentLanguage === "hr" ? "Nema zapisa za odabrane filtre." : "No entries match these filters.";
      empty.append(icon, label);
      list.append(empty);
    } else {
      items.forEach((item) => list.append(createContentRow(item)));
    }
    refreshIcons();
  }

  function openPipelineStage(status, kind = "all") {
    contentKind = kind;
    contentStatus = status;
    const statusFilter = document.querySelector("#content-status-filter");
    if (statusFilter) statusFilter.value = status;
    contentSearch = "";
    const search = document.querySelector("#content-search");
    if (search) search.value = "";
    renderContent();
    navigateTo("content");
  }

  async function loadContent() {
    contentSearch = document.querySelector("#content-search")?.value.trim() || "";
    contentStatus = document.querySelector("#content-status-filter")?.value || "";
    contentItems = await apiRequest(`/projects/${projectID}/content`);
    renderContent();
    hydrateContentEditors();
    renderDashboardContent();
    renderWebsiteIntegration();
    hydrateSocialStudio();
  }

  function strategyContextSummary(strategy) {
    if (!strategy) return currentLanguage === "hr" ? "Nema spremljenog konteksta." : "No saved context.";
    const parts = [];
    if (strategy.sourceFilename) parts.push(strategy.sourceFilename);
    if (strategy.audience) parts.push(`${currentLanguage === "hr" ? "Publika" : "Audience"}: ${strategy.audience}`);
    if (strategy.priorityTopics?.length) parts.push(`${currentLanguage === "hr" ? "Teme" : "Topics"}: ${strategy.priorityTopics.join(", ")}`);
    return parts.join(" · ") || strategy.sixMonthGoal || (currentLanguage === "hr" ? "Kontekst je spreman za dopunu." : "Context is ready to be completed.");
  }

  function renderStrategyContext() {
    const label = document.querySelector("#content-strategy-label");
    const summary = document.querySelector("#content-strategy-summary");
    if (label) {
      label.textContent = projectStrategy?.sourceFilename
        ? `${currentLanguage === "hr" ? "Strategija iz datoteke" : "File strategy"} · rev ${projectStrategy.revision}`
        : `${currentLanguage === "hr" ? "Ručni strateški kontekst" : "Manual strategy context"} · rev ${projectStrategy?.revision || 0}`;
    }
    if (summary) summary.textContent = strategyContextSummary(projectStrategy);
    const status = document.querySelector("#strategy-context-status span");
    if (status && projectStrategy) {
      status.textContent = projectStrategy.sourceFilename
        ? `${projectStrategy.sourceFilename} · ${currentLanguage === "hr" ? "tekst izdvojen i spremljen" : "text extracted and saved"}`
        : `${currentLanguage === "hr" ? "Ručni kontekst spremljen" : "Manual context saved"} · rev ${projectStrategy.revision}`;
    }
    renderSettingsStrategy();
    hydrateSocialStudio();
  }

  function renderSettingsStrategy() {
    setText("#settings-strategy-goal", projectStrategy?.sixMonthGoal || projectStrategy?.primaryGoals?.[0] || "—");
    setText("#settings-strategy-audience", projectPersonas.find((persona) => persona.isPrimary)?.name || projectStrategy?.audience || "—");
    const cadence = projectProfile?.newsletterCadence || "weekly";
    setText("#settings-strategy-cadence", `${projectProfile?.socialPostsPerWeek ?? 0} ${currentLanguage === "hr" ? "objava/tjedno" : "posts/week"} · ${cadence}`);
    document.querySelectorAll("[data-strategy-status]").forEach((node) => {
      node.textContent = projectStrategy?.revision ? `${currentLanguage === "hr" ? "Aktivna" : "Active"} · rev ${projectStrategy.revision}` : (currentLanguage === "hr" ? "Nije dovršena" : "Incomplete");
      node.className = `status-pill ${projectStrategy?.revision ? "auto" : "review"}`;
    });
  }

  function hydrateStrategyForm() {
    if (!projectStrategy) return;
    const suggestedTopicValues = [...document.querySelectorAll("[data-strategy-topics] button:not(#strategy-topic-add)")]
      .flatMap((button) => [button.dataset.hr, button.dataset.en])
      .filter(Boolean)
      .map((value) => value.toLocaleLowerCase("hr"));
    const freeformTopics = (projectStrategy.priorityTopics || []).filter((value) => !suggestedTopicValues.includes(String(value).toLocaleLowerCase("hr")));
    const values = {
      sixMonthGoal: projectStrategy.sixMonthGoal,
      priorityTopics: freeformTopics.join(", "),
      audienceProblem: projectStrategy.audienceProblem,
      brandMessage: projectStrategy.brandMessage,
      proofPoints: projectStrategy.proofPoints,
      forbiddenTopics: projectStrategy.forbiddenTopics,
      successMetrics: projectStrategy.successMetrics,
    };
    Object.entries(values).forEach(([field, value]) => {
      const input = document.querySelector(`[data-strategy-field="${field}"]`);
      if (input) input.value = value || "";
    });
    const matchesStoredValue = (button, value) => [button.dataset.strategyValue, button.dataset.hr, button.dataset.en, button.textContent]
      .filter(Boolean)
      .some((candidate) => candidate.trim().toLocaleLowerCase("hr") === String(value).trim().toLocaleLowerCase("hr"));
    const hydrateChoices = (selector, storedValues, addSelector) => {
      const root = document.querySelector(selector);
      if (!root) return;
      const values = (storedValues || []).filter(Boolean);
      root.querySelectorAll("button:not([id])").forEach((button) => {
        button.classList.toggle("active", values.some((value) => matchesStoredValue(button, value)));
      });
      values.forEach((value) => {
        if ([...root.querySelectorAll("button")].some((button) => matchesStoredValue(button, value))) return;
        const button = document.createElement("button");
        button.type = "button";
        button.className = "active";
        button.dataset.strategyValue = value;
        button.textContent = value;
        root.insertBefore(button, root.querySelector(addSelector));
      });
    };
    hydrateChoices("[data-strategy-goals]", projectStrategy.primaryGoals, "#strategy-goal-add");
    hydrateChoices("[data-strategy-topics]", projectStrategy.priorityTopics, "#strategy-topic-add");

    if (projectStrategy.audience) {
      const personas = document.querySelector("#strategy-personas");
      let selected = [...(personas?.querySelectorAll("[data-strategy-persona]") || [])]
        .find((button) => matchesStoredValue(button, projectStrategy.audience));
      if (!selected && personas) {
        selected = createStrategyPersona(projectStrategy.audience);
      }
      personas?.querySelectorAll("[data-strategy-persona]").forEach((button) => button.classList.toggle("selected", button === selected));
    }

    const toneValues = Object.fromEntries(String(projectStrategy.tone || "").split(",").map((pair) => pair.trim().split(":")));
    document.querySelectorAll("[data-strategy-tone]").forEach((input) => {
      const value = Number(toneValues[input.dataset.strategyTone]);
      if (Number.isFinite(value)) input.value = String(Math.max(0, Math.min(100, value)));
    });
    if (projectStrategy.mode) {
      document.querySelectorAll("[data-strategy-mode]").forEach((button) => {
        const selected = button.dataset.strategyMode === projectStrategy.mode;
        button.classList.toggle("selected", selected);
        button.setAttribute("aria-pressed", String(selected));
      });
      restoreDerivedState();
    }
    const status = document.querySelector(".strategy-file-status");
    const filename = document.querySelector(".strategy-file-name");
    if (status && filename && projectStrategy.sourceFilename) {
      status.hidden = false;
      filename.textContent = projectStrategy.sourceFilename;
    }
    refreshIcons();
  }

  function createStrategyPersona(name) {
    const personas = document.querySelector("#strategy-personas");
    const add = document.querySelector("#strategy-persona-add");
    if (!personas || !add || !name?.trim()) return null;
    const cleanName = name.trim().slice(0, 160);
    const button = document.createElement("button");
    button.type = "button";
    button.className = "persona selected";
    button.dataset.strategyPersona = cleanName;
    const avatar = document.createElement("span");
    avatar.className = "persona-avatar";
    avatar.textContent = cleanName.split(/\s+/).slice(0, 2).map((part) => part[0]).join("").toUpperCase();
    const copy = document.createElement("span");
    copy.className = "persona-copy";
    const title = document.createElement("strong");
    title.textContent = cleanName;
    const detail = document.createElement("small");
    detail.textContent = currentLanguage === "hr" ? "Projektna publika" : "Project audience";
    copy.append(title, detail);
    const icon = document.createElement("i");
    icon.dataset.lucide = "check";
    button.append(avatar, copy, icon);
    personas.insertBefore(button, add);
    return button;
  }

  function renderProjectPersonas() {
    const root = document.querySelector("#strategy-personas");
    const add = document.querySelector("#strategy-persona-add");
    if (!root || !add) return;
    [...root.children].forEach((child) => { if (child !== add) child.remove(); });
    projectPersonas.forEach((persona) => {
      const entry = document.createElement("div");
      entry.className = "persona-entry";
      const button = document.createElement("button");
      button.type = "button";
      button.className = `persona ${persona.isPrimary ? "selected" : ""}`;
      button.dataset.personaSelect = persona.id;
      button.dataset.strategyPersona = persona.name;
      const avatar = document.createElement("span");
      avatar.className = "persona-avatar";
      avatar.textContent = persona.name.split(/\s+/).slice(0, 2).map((part) => part[0]).join("").toUpperCase();
      const copy = document.createElement("span");
      copy.className = "content-main";
      copy.className = "persona-copy";
      const title = document.createElement("strong");
      title.textContent = persona.name;
      const detail = document.createElement("small");
      detail.textContent = persona.demographics || persona.description || (currentLanguage === "hr" ? "Projektna publika" : "Project audience");
      copy.append(title, detail);
      const check = document.createElement("i");
      check.dataset.lucide = persona.isPrimary ? "check" : "circle";
      button.append(avatar, copy, check);
      const actions = document.createElement("span");
      actions.className = "persona-actions";
      const edit = document.createElement("button");
      edit.type = "button";
      edit.dataset.personaEdit = persona.id;
      edit.setAttribute("aria-label", currentLanguage === "hr" ? "Uredi publiku" : "Edit audience");
      const editIcon = document.createElement("i");
      editIcon.dataset.lucide = "pencil";
      edit.append(editIcon);
      const remove = document.createElement("button");
      remove.type = "button";
      remove.dataset.personaDelete = persona.id;
      remove.setAttribute("aria-label", currentLanguage === "hr" ? "Obriši publiku" : "Delete audience");
      const removeIcon = document.createElement("i");
      removeIcon.dataset.lucide = "trash-2";
      remove.append(removeIcon);
      actions.append(edit, remove);
      entry.append(button, actions);
      root.insertBefore(entry, add);
    });
    if (hydrated) applyRoleUI();
    refreshIcons();
  }

  async function loadProjectPersonas() {
    projectPersonas = await apiRequest(`/projects/${projectID}/personas`);
    renderProjectPersonas();
    renderSettingsStrategy();
  }

  function personaPayload(persona, changes = {}) {
    return {
      name: changes.name ?? persona?.name ?? "",
      description: changes.description ?? persona?.description ?? "",
      demographics: changes.demographics ?? persona?.demographics ?? "",
      isPrimary: changes.isPrimary ?? persona?.isPrimary ?? projectPersonas.length === 0,
      metadata: { ...(persona?.metadata || {}), editor: "strategy-setup" },
    };
  }

  async function createProjectPersona() {
    if (!projectRoleAllows("owner", "lead", "editor")) return;
    const name = window.prompt(currentLanguage === "hr" ? "Naziv publike / persone" : "Audience / persona name", "");
    if (!name?.trim()) return;
    const demographics = window.prompt(currentLanguage === "hr" ? "Segment ili demografija (neobavezno)" : "Segment or demographics (optional)", "") || "";
    const description = window.prompt(currentLanguage === "hr" ? "Opis potreba i motivacije (neobavezno)" : "Needs and motivations (optional)", "") || "";
    try {
      await apiRequest(`/projects/${projectID}/personas`, {
        method: "POST",
        body: JSON.stringify(personaPayload(null, { name: name.trim(), demographics: demographics.trim(), description: description.trim() })),
      });
      await loadProjectPersonas();
      await saveStrategy();
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Spremanje publike" : "Saving audience", error);
    }
  }

  async function updateProjectPersona(persona) {
    if (!projectRoleAllows("owner", "lead", "editor")) return;
    if (!persona) return;
    const name = window.prompt(currentLanguage === "hr" ? "Naziv publike / persone" : "Audience / persona name", persona.name);
    if (!name?.trim()) return;
    const demographics = window.prompt(currentLanguage === "hr" ? "Segment ili demografija" : "Segment or demographics", persona.demographics || "") ?? persona.demographics;
    const description = window.prompt(currentLanguage === "hr" ? "Opis potreba i motivacije" : "Needs and motivations", persona.description || "") ?? persona.description;
    try {
      await apiRequest(`/projects/${projectID}/personas/${persona.id}`, {
        method: "PUT",
        body: JSON.stringify(personaPayload(persona, { name: name.trim(), demographics: demographics.trim(), description: description.trim() })),
      });
      await loadProjectPersonas();
      await saveStrategy();
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Uređivanje publike" : "Editing audience", error);
    }
  }

  async function selectProjectPersona(persona) {
    if (!projectRoleAllows("owner", "lead", "editor")) return;
    if (!persona || persona.isPrimary) return;
    try {
      await apiRequest(`/projects/${projectID}/personas/${persona.id}`, {
        method: "PUT", body: JSON.stringify(personaPayload(persona, { isPrimary: true })),
      });
      await loadProjectPersonas();
      await saveStrategy();
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Odabir primarne publike" : "Selecting primary audience", error);
    }
  }

  async function deleteProjectPersona(persona) {
    if (!projectRoleAllows("owner", "lead")) return;
    if (!persona) return;
    const confirmed = window.confirm(currentLanguage === "hr" ? `Obrisati publiku „${persona.name}”?` : `Delete audience “${persona.name}”?`);
    if (!confirmed) return;
    try {
      await apiRequest(`/projects/${projectID}/personas/${persona.id}`, { method: "DELETE" });
      await loadProjectPersonas();
      await saveStrategy();
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Brisanje publike" : "Deleting audience", error);
    }
  }

  async function loadStrategy() {
    projectStrategy = await apiRequest(`/projects/${projectID}/strategy`);
    hydrateStrategyForm();
    renderStrategyContext();
  }

  async function loadAIStatus() {
    contentAIStatus = await apiRequest(`/projects/${projectID}/content/ai/status`);
    const provider = contentAIStatus.model
      ? `${contentAIStatus.provider} · ${contentAIStatus.model}`
      : (currentLanguage === "en" ? "Local AI · no account" : "Local AI · bez računa");
    const banner = document.querySelector("#content-ai-provider");
    const modal = document.querySelector("#content-ai-modal-provider");
    if (banner) banner.textContent = provider;
    if (modal) modal.textContent = provider;
  }

  function selectedButtonLabels(selector) {
    return [...document.querySelectorAll(`${selector} button.active`)]
      .filter((button) => !button.querySelector('[data-lucide="plus"]'))
      .map((button) => button.textContent.trim())
      .filter(Boolean);
  }

  function collectStrategyInput() {
    const field = (name) => document.querySelector(`[data-strategy-field="${name}"]`)?.value.trim() || "";
    const topics = [...field("priorityTopics").split(","), ...selectedButtonLabels("[data-strategy-topics]")]
      .map((value) => value.trim()).filter(Boolean);
    const toneValues = [...document.querySelectorAll("[data-strategy-tone]")].map((input) => `${input.dataset.strategyTone}:${input.value}`);
    return {
      mode: document.querySelector("[data-strategy-mode].selected")?.dataset.strategyMode || "questions",
      sixMonthGoal: field("sixMonthGoal"),
      primaryGoals: selectedButtonLabels("[data-strategy-goals]"),
      priorityTopics: [...new Set(topics)],
      audience: document.querySelector(".persona.selected")?.dataset.strategyPersona || document.querySelector(".persona.selected strong")?.textContent.trim() || projectStrategy?.audience || "",
      audienceProblem: field("audienceProblem"),
      brandMessage: field("brandMessage"),
      proofPoints: field("proofPoints"),
      forbiddenTopics: field("forbiddenTopics"),
      successMetrics: field("successMetrics"),
      tone: toneValues.join(", "),
    };
  }

  async function saveStrategy() {
    if (!projectID || !projectRoleAllows("owner", "lead", "editor")) return null;
    window.clearTimeout(strategySaveTimer);
    const targetProjectID = projectID;
    const requestVersion = ++strategyChangeVersion;
    const payload = collectStrategyInput();
    const button = document.querySelector("#strategy-save");
    strategySavePending += 1;
    if (button) button.disabled = true;
    const queuedSave = strategySaveQueue.then(() => apiRequest(`/projects/${targetProjectID}/strategy`, {
      method: "PUT",
      body: JSON.stringify(payload),
    }));
    strategySaveQueue = queuedSave.then(() => undefined, () => undefined);
    try {
      const saved = await queuedSave;
      if (requestVersion === strategyChangeVersion && targetProjectID === projectID) {
        projectStrategy = saved;
        renderStrategyContext();
        showToast("strategySaved");
      }
      return saved;
    } catch (error) {
      console.error("Millena strategy save failed", error);
      if (requestVersion === strategyChangeVersion && targetProjectID === projectID) showToast("strategyError");
      return null;
    } finally {
      strategySavePending = Math.max(0, strategySavePending - 1);
      if (button) {
        const locked = Boolean(button.dataset.lockFeature || button.dataset.lockRole || button.dataset.lockChannelLimit);
        button.disabled = strategySavePending > 0 || locked;
      }
    }
  }

  function scheduleStrategySave() {
    if (!projectID || !hydrated || !projectRoleAllows("owner", "lead", "editor")) return;
    strategyChangeVersion += 1;
    window.clearTimeout(strategySaveTimer);
    strategySaveTimer = window.setTimeout(saveStrategy, 900);
  }

  async function uploadStrategyFile(input) {
    const file = input.files?.[0];
    if (!file || !projectID) return;
    window.clearTimeout(strategySaveTimer);
    const targetProjectID = projectID;
    const requestVersion = ++strategyChangeVersion;
    const uploadToken = ++strategyUploadToken;
    const status = document.querySelector(".strategy-file-status");
    const detail = status?.querySelector("small");
    if (status) status.hidden = false;
    if (detail) detail.textContent = currentLanguage === "hr" ? "Izdvajam tekst i spremam kontekst…" : "Extracting text and saving context…";
    const form = new FormData();
    form.append("file", file);
    const button = document.querySelector("#strategy-save");
    strategySavePending += 1;
    if (button) button.disabled = true;
    const queuedUpload = strategySaveQueue.then(() => apiRequest(`/projects/${targetProjectID}/strategy/file`, { method: "POST", body: form }));
    strategySaveQueue = queuedUpload.then(() => undefined, () => undefined);
    try {
      const saved = await queuedUpload;
      if (uploadToken === strategyUploadToken && targetProjectID === projectID && detail) {
        detail.textContent = currentLanguage === "hr" ? "Tekst je izdvojen i koristi se za AI" : "Text extracted and used by AI";
      }
      if (requestVersion === strategyChangeVersion && targetProjectID === projectID) {
        projectStrategy = saved;
        renderStrategyContext();
        showToast("strategyFileSaved");
      }
    } catch (error) {
      if (uploadToken === strategyUploadToken && targetProjectID === projectID && detail) detail.textContent = error.message;
      console.error("Millena strategy upload failed", error);
      if (requestVersion === strategyChangeVersion && targetProjectID === projectID) showToast("strategyError");
    } finally {
      strategySavePending = Math.max(0, strategySavePending - 1);
      if (button) {
        const locked = Boolean(button.dataset.lockFeature || button.dataset.lockRole || button.dataset.lockChannelLimit);
        button.disabled = strategySavePending > 0 || locked;
      }
    }
    refreshIcons();
  }

  function resetContentDeleteButton() {
    const button = document.querySelector("#content-delete");
    if (!button) return;
    button.dataset.confirm = "false";
    button.querySelector("span").textContent = currentLanguage === "hr" ? "Obriši zapis" : "Delete entry";
  }

  function openContentModal(item = null) {
    const modal = document.querySelector("#content-modal");
    if (!modal) return;
    const defaultKind = contentKind !== "all" ? contentKind : "social";
    document.querySelector("#content-item-id").value = item?.id || "";
    document.querySelector("#content-item-source").value = item?.source || "manual";
    document.querySelector("#content-item-title").value = item?.title || "";
    document.querySelector("#content-item-kind").value = item?.kind || defaultKind;
    document.querySelector("#content-item-status").value = item?.status || "draft";
    document.querySelector("#content-item-summary").value = item?.summary || "";
    document.querySelector("#content-item-body").value = item?.body || "";
    document.querySelector("#content-item-channels").value = (item?.channels || []).join(", ");
    document.querySelector("#content-item-scheduled").value = item?.scheduledFor ? localDateTimeValue(item.scheduledFor) : "";
    document.querySelector("#content-ai-brief").value = "";
    document.querySelector("#content-delete").hidden = !item;
    const approveReview = document.querySelector("#content-approve-review");
    if (approveReview) approveReview.hidden = !item || item.status !== "in_review" || !projectRoleAllows("owner", "lead", "editor");
    const returnReview = document.querySelector("#content-return-review");
    if (returnReview) returnReview.hidden = !item || item.status !== "in_review" || !projectRoleAllows("owner", "lead", "editor");
    const reviewComment = document.querySelector("#content-review-comment");
    if (reviewComment) reviewComment.value = "";
    const reviewCommentWrap = document.querySelector("#content-review-comment-wrap");
    if (reviewCommentWrap) reviewCommentWrap.hidden = !item || item.status !== "in_review" || !projectRoleAllows("owner", "lead", "editor");
    const reviewMeta = document.querySelector("#content-review-meta");
    if (reviewMeta) {
      const reviewLabel = contentReviewLabel(item);
      reviewMeta.hidden = !reviewLabel;
      reviewMeta.textContent = reviewLabel;
    }
    document.querySelector("#content-modal-title").textContent = item
      ? (currentLanguage === "hr" ? "Uredi sadržaj" : "Edit content")
      : (currentLanguage === "hr" ? "Novi sadržaj" : "New content");
    const result = document.querySelector("#content-ai-result");
    if (result) result.hidden = true;
    resetContentDeleteButton();
    modal.classList.add("open");
    modal.setAttribute("aria-hidden", "false");
    document.body.style.overflow = "hidden";
    (item ? document.querySelector("#content-item-title") : document.querySelector("#content-ai-brief"))?.focus();
  }

  function closeContentModal() {
    const modal = document.querySelector("#content-modal");
    if (!modal) return;
    modal.classList.remove("open");
    modal.setAttribute("aria-hidden", "true");
    document.body.style.overflow = "";
  }

  function openAccountModal() {
    if (!sessionUser) return;
    const initials = sessionUser.displayName.split(/\s+/).filter(Boolean).slice(0, 2).map((part) => part[0]?.toUpperCase()).join("") || "AD";
    document.querySelector("#account-modal-avatar").textContent = initials;
    document.querySelector("#account-modal-name").textContent = sessionUser.displayName;
    document.querySelector("#account-modal-email").textContent = sessionUser.email;
    document.querySelector("#account-display-name").value = sessionUser.displayName;
    document.querySelector("#account-current-password").value = "";
    document.querySelector("#account-new-password").value = "";
    openDomainModal("account-modal");
    document.querySelector("#account-display-name")?.focus();
  }

  function closeAccountModal() {
    closeDomainModal("account-modal");
  }

  async function saveAccount() {
    const button = document.querySelector("#account-save");
    const displayName = document.querySelector("#account-display-name")?.value.trim() || "";
    const currentPassword = document.querySelector("#account-current-password")?.value || "";
    const newPassword = document.querySelector("#account-new-password")?.value || "";
    if (!displayName || (newPassword && !currentPassword)) {
      (newPassword && !currentPassword ? document.querySelector("#account-current-password") : document.querySelector("#account-display-name"))?.focus();
      return;
    }
    if (button) button.disabled = true;
    try {
      const user = await apiRequest("/auth/account", { method: "PUT", body: JSON.stringify({ displayName, currentPassword, newPassword }) });
      sessionUser = user;
      renderIdentity();
      closeAccountModal();
      showToast("contentSaved");
    } catch (error) {
      console.error("Millena account update failed", error);
      showDomainError(currentLanguage === "hr" ? "Spremanje profila" : "Saving profile", error);
    } finally {
      if (button) button.disabled = false;
    }
  }

  function contentPayload() {
    const id = document.querySelector("#content-item-id").value;
    const existing = contentItems.find((item) => item.id === id) || null;
    const status = document.querySelector("#content-item-status").value;
    const scheduled = document.querySelector("#content-item-scheduled").value;
    return {
      kind: document.querySelector("#content-item-kind").value,
      status,
      title: document.querySelector("#content-item-title").value.trim(),
      summary: document.querySelector("#content-item-summary").value.trim(),
      body: document.querySelector("#content-item-body").value.trim(),
      channels: document.querySelector("#content-item-channels").value.split(",").map((value) => value.trim()).filter(Boolean),
      scheduledFor: scheduled ? new Date(scheduled).toISOString() : null,
      source: document.querySelector("#content-item-source").value || "manual",
      metadata: {
        ...(existing?.metadata || {}),
        editor: "content-modal",
        plan: projectAccess?.entitlement?.planCode || "unknown",
      },
    };
  }

  async function saveContentItem() {
    const id = document.querySelector("#content-item-id").value;
    const title = document.querySelector("#content-item-title");
    const scheduled = document.querySelector("#content-item-scheduled");
    const status = document.querySelector("#content-item-status").value;
    if (!title.value.trim()) {
      title.reportValidity();
      return;
    }
    scheduled.required = status === "scheduled";
    if (scheduled.required && !scheduled.value) {
      scheduled.reportValidity();
      return;
    }
    const save = document.querySelector("#content-save");
    if (save) save.disabled = true;
    try {
      await apiRequest(id ? `/projects/${projectID}/content/items/${id}` : `/projects/${projectID}/content/items`, {
        method: id ? "PUT" : "POST", body: JSON.stringify(contentPayload()),
      });
      closeContentModal();
      await loadContent();
      await loadDashboard();
      showToast("contentSaved");
    } catch (error) {
      console.error("Millena content save failed", error);
      showToast("contentError");
    } finally {
      if (save) save.disabled = false;
    }
  }

  async function approveContentReview() {
    const id = document.querySelector("#content-item-id")?.value;
    const button = document.querySelector("#content-approve-review");
    if (!id || !button) return;
    button.disabled = true;
    try {
      await apiRequest(`/projects/${projectID}/content/items/${id}/review`, { method: "POST" });
      closeContentModal();
      await Promise.all([loadContent(), loadDashboard()]);
      showToast("contentSaved");
    } catch (error) {
      console.error("Millena content review failed", error);
      showToast("contentError");
    } finally {
      button.disabled = false;
    }
  }

  async function returnContentForRevision() {
    const id = document.querySelector("#content-item-id")?.value;
    const button = document.querySelector("#content-return-review");
    const comment = document.querySelector("#content-review-comment")?.value.trim();
    if (!id || !button || !comment) {
      document.querySelector("#content-review-comment")?.focus();
      return;
    }
    button.disabled = true;
    try {
      await apiRequest(`/projects/${projectID}/content/items/${id}/return-for-revision`, { method: "POST", body: JSON.stringify({ comment }) });
      closeContentModal();
      await Promise.all([loadContent(), loadDashboard()]);
      showToast("contentSaved");
    } catch (error) {
      console.error("Millena content revision request failed", error);
      showToast("contentError");
    } finally {
      button.disabled = false;
    }
  }

  async function deleteContentItem() {
    const id = document.querySelector("#content-item-id").value;
    const button = document.querySelector("#content-delete");
    if (!id || !button) return;
    if (button.dataset.confirm !== "true") {
      button.dataset.confirm = "true";
      button.querySelector("span").textContent = currentLanguage === "hr" ? "Klikni ponovno za potvrdu" : "Click again to confirm";
      window.setTimeout(resetContentDeleteButton, 4000);
      return;
    }
    button.disabled = true;
    try {
      await apiRequest(`/projects/${projectID}/content/items/${id}`, { method: "DELETE" });
      if (id === blogContentID) newBlog();
      if (id === newsletterContentID) newNewsletter();
      closeContentModal();
      await loadContent();
      await loadDashboard();
      showToast("contentDeleted");
    } catch (error) {
      console.error("Millena content delete failed", error);
      showToast("contentError");
    } finally {
      button.disabled = false;
      resetContentDeleteButton();
    }
  }

  async function runContentAI(operation) {
    const brief = document.querySelector("#content-ai-brief");
    const body = document.querySelector("#content-item-body");
    if (operation === "generate" && !brief.value.trim()) {
      brief.setCustomValidity(currentLanguage === "hr" ? "Upišite temu ili zadatak za AI." : "Enter a topic or task for AI.");
      brief.reportValidity();
      brief.setCustomValidity("");
      return;
    }
    if (operation === "refine" && !body.value.trim()) {
      body.setCustomValidity(currentLanguage === "hr" ? "Prvo napišite tekst koji AI treba doraditi." : "Write the copy AI should refine first.");
      body.reportValidity();
      body.setCustomValidity("");
      return;
    }
    const button = document.querySelector(operation === "generate" ? "#content-ai-generate" : "#content-ai-refine");
    const buttons = [...document.querySelectorAll(".content-ai-actions button")];
    buttons.forEach((choice) => { choice.disabled = true; });
    button?.classList.add("loading");
    try {
      const result = await apiRequest(`/projects/${projectID}/content/ai`, {
        method: "POST",
        body: JSON.stringify({
          operation,
          kind: document.querySelector("#content-item-kind").value,
          brief: brief.value.trim(),
          title: document.querySelector("#content-item-title").value.trim(),
          body: body.value.trim(),
          language: socialVariantLocale(),
        }),
      });
      document.querySelector("#content-item-title").value = result.title;
      body.value = result.body;
      const summary = document.querySelector("#content-item-summary");
      if (summary && !summary.value.trim()) summary.value = aiGeneratedSummary(result.title, result.body);
      const channels = document.querySelector("#content-item-channels");
      if (operation === "generate" && channels && !channels.value.trim()) {
        channels.value = defaultContentChannels(document.querySelector("#content-item-kind").value).join(", ");
      }
      document.querySelector("#content-item-source").value = "ai";
      const resultBox = document.querySelector("#content-ai-result");
      if (resultBox) {
        resultBox.hidden = false;
        resultBox.querySelector("span").textContent = `${result.provider} · ${result.contextSummary}${result.warning ? ` · ${result.warning}` : ""}`;
      }
      showToast(operation === "generate" ? "contentGenerated" : "contentRefined");
    } catch (error) {
      console.error("Millena AI content operation failed", error);
      showToast("contentAIError");
    } finally {
      buttons.forEach((choice) => { choice.disabled = false; });
      button?.classList.remove("loading");
    }
  }

  function setText(selector, value) {
    const node = document.querySelector(selector);
    if (node) node.textContent = value == null ? "—" : String(value);
  }

  function formatDateTime(value, options = {}) {
    if (!value) return currentLanguage === "hr" ? "Nije pokrenuto" : "Not run";
    return new Intl.DateTimeFormat(currentLanguage === "hr" ? "hr-HR" : "en-GB", {
      day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit", ...options,
    }).format(new Date(value));
  }

  function openDomainModal(id) {
    const modal = document.querySelector(`#${id}`);
    if (!modal) return;
    modal.classList.add("open");
    modal.setAttribute("aria-hidden", "false");
    document.body.style.overflow = "hidden";
  }

  function closeDomainModal(id) {
    const modal = document.querySelector(`#${id}`);
    if (!modal) return;
    modal.classList.remove("open");
    modal.setAttribute("aria-hidden", "true");
    if (!document.querySelector(".modal-backdrop.open")) document.body.style.overflow = "";
  }

  function showDomainError(context, error) {
    console.error(`Millena ${context} failed`, error);
    openActionModal(
      currentLanguage === "hr" ? "Zahtjev nije dovršen" : "Request not completed",
      `${context}: ${error.message}`,
      currentLanguage === "hr" ? "Provjerite podatke" : "Check the input",
    );
  }

  function ensureSelectValue(select, value, label = value) {
    if (!select || !value) return;
    if (![...select.options].some((option) => option.value === value)) {
      select.add(new Option(label, value));
    }
    select.value = value;
  }

  function setupCompletionPercent() {
    const checks = [
      projectProfile?.projectName,
      projectProfile?.companyName,
      projectProfile?.primaryLanguage,
      projectStrategy?.sixMonthGoal || projectStrategy?.sourceFilename,
      projectPersonas.length || projectStrategy?.audience,
      channelConnections.length || socialConnections.size,
    ];
    return Math.round((checks.filter(Boolean).length / checks.length) * 100);
  }

  function renderDashboardContent() {
    const list = document.querySelector("#dashboard-content-list");
    if (!list) return;
    list.replaceChildren();
    const recent = [...contentItems]
      .sort((left, right) => new Date(right.updatedAt) - new Date(left.updatedAt))
      .slice(0, 3);
    if (!recent.length) {
      const empty = document.createElement("p");
      empty.className = "content-empty";
      empty.textContent = currentLanguage === "hr" ? "Još nema sadržajnih zapisa." : "There are no content entries yet.";
      list.append(empty);
      return;
    }
    recent.forEach((item) => {
      const row = document.createElement("article");
      row.className = "content-row";
      row.dataset.dashboardContentId = item.id;
      const thumb = document.createElement("span");
      thumb.className = `doc-thumb ${item.kind}`;
      const icon = document.createElement("i");
      icon.dataset.lucide = contentKindIcon(item.kind);
      thumb.append(icon);
      const copy = document.createElement("span");
      copy.className = "content-main dashboard-content-main";
      const title = document.createElement("strong");
      title.textContent = item.title;
      const excerpt = document.createElement("small");
      excerpt.className = "dashboard-content-excerpt";
      excerpt.textContent = item.summary || item.body?.replace(/\s+/g, " ").trim().slice(0, 110) || (currentLanguage === "hr" ? "Sadržaj je spreman za uređivanje." : "Content is ready to edit.");
      const meta = document.createElement("span");
      meta.className = "dashboard-content-meta";
      const kind = document.createElement("span");
      kind.className = `dashboard-kind-pill ${item.kind}`;
      kind.title = contentKindLabel(item.kind);
      kind.setAttribute("aria-label", contentKindLabel(item.kind));
      const kindIcon = document.createElement("i");
      kindIcon.dataset.lucide = contentKindIcon(item.kind);
      kind.append(kindIcon);
      meta.append(kind);
      (item.channels || []).slice(0, 2).forEach((channel) => {
        const channelTag = document.createElement("span");
        channelTag.className = "dashboard-channel-tag";
        channelTag.textContent = channel.replace(/(^|[_-])(\w)/g, (_, prefix, letter) => `${prefix ? " " : ""}${letter.toUpperCase()}`);
        meta.append(channelTag);
      });
      const reviewLabel = contentReviewLabel(item);
      if (reviewLabel) {
        const review = document.createElement("small");
        review.className = "dashboard-review-note";
        review.textContent = reviewLabel;
        meta.append(review);
      }
      copy.append(title, excerpt, meta);
      const status = document.createElement("span");
      status.className = `status-pill ${contentStatusClass(item.status)}`;
      status.textContent = contentStatusLabel(item.status);
      const updated = document.createElement("small");
      updated.textContent = formatDateTime(item.updatedAt);
      const arrow = document.createElement("i");
      arrow.dataset.lucide = "chevron-right";
      row.append(thumb, copy, status, updated, arrow);
      list.append(row);
    });
    refreshIcons();
  }

  function renderDashboard() {
    if (!dashboardData) return;
    const locale = currentLanguage === "hr" ? "hr-HR" : "en-GB";
    const overview = document.querySelector('[data-screen="overview"]');
    const headingDate = overview?.querySelector(".page-heading .eyebrow");
    const headingTitle = overview?.querySelector(".page-heading h1");
    const headingCopy = overview?.querySelector(".page-heading h1 + p");
    if (headingDate) headingDate.textContent = new Intl.DateTimeFormat(locale, { weekday: "long", day: "numeric", month: "long" }).format(new Date());
    if (headingTitle) headingTitle.textContent = `${currentLanguage === "hr" ? "Dobro došli" : "Welcome"}, ${sessionUser?.displayName?.split(/\s+/)[0] || ""}.`;
    if (headingCopy) headingCopy.textContent = `${dashboardData.stats?.scheduledNext14Days || 0} ${currentLanguage === "hr" ? "zakazanih stavki u idućih 14 dana" : "items scheduled in the next 14 days"} · ${dashboardData.stats?.waitingReview || 0} ${currentLanguage === "hr" ? "čeka pregled" : "waiting for review"}.`;
    setText("#dashboard-published", dashboardData.stats?.publishedThisMonth || 0);
    setText("#dashboard-scheduled", dashboardData.stats?.scheduledNext14Days || 0);
    setText("#dashboard-review", dashboardData.stats?.waitingReview || 0);
    setText("#dashboard-audience", dashboardData.stats?.newsletterAudience || 0);
    const completion = setupCompletionPercent();
    setText("#setup-completion", `${completion}%`);
    const meter = document.querySelector(".setup-meter b");
    if (meter) meter.style.width = `${completion}%`;
    const setupMissing = [
      [projectProfile?.companyName, currentLanguage === "hr" ? "profil tvrtke" : "company profile"],
      [projectStrategy?.sixMonthGoal || projectStrategy?.sourceFilename, currentLanguage === "hr" ? "strateški kontekst" : "strategy context"],
      [projectPersonas.length, currentLanguage === "hr" ? "primarna publika" : "primary audience"],
      [channelConnections.length || socialConnections.size, currentLanguage === "hr" ? "barem jedan kanal" : "at least one channel"],
    ].filter(([ready]) => !ready).map(([, label]) => label);
    setText("#setup-banner-title", completion >= 100 ? (currentLanguage === "hr" ? "Projekt je postavljen" : "Project setup complete") : (currentLanguage === "hr" ? "Dovršite postavljanje projekta" : "Finish project setup"));
    setText("#setup-banner-detail", setupMissing.length ? `${currentLanguage === "hr" ? "Nedostaje" : "Missing"}: ${setupMissing.join(", ")}.` : (currentLanguage === "hr" ? "Profil, kontekst, publika i kanali spremni su za rad." : "Profile, context, audience, and channels are ready."));
    const indicator = document.querySelector("#notification-indicator");
    if (indicator) {
      const needsAttention = (dashboardData.stats?.waitingReview || 0) + contentItems.filter((item) => item.status === "failed").length;
      indicator.hidden = needsAttention === 0;
      indicator.title = `${needsAttention} ${currentLanguage === "hr" ? "stavki traži pažnju" : "items need attention"}`;
    }
    const automationCopy = document.querySelector(".automation-summary .panel-head p");
    if (automationCopy) {
      automationCopy.textContent = `${dashboardData.automation?.enabledRules || 0}/${dashboardData.automation?.totalRules || 0} ${currentLanguage === "hr" ? "pravila uključeno" : "rules enabled"} · ${dashboardData.automation?.runCount || 0} ${currentLanguage === "hr" ? "pokretanja" : "runs"}`;
    }
    const liveLabel = document.querySelector(".automation-summary .live-pill span");
    if (liveLabel) liveLabel.textContent = dashboardData.automation?.enabledRules ? (currentLanguage === "hr" ? "Aktivno" : "Live") : (currentLanguage === "hr" ? "Pauzirano" : "Paused");
    const automationRun = document.querySelector(".automation-summary .automation-run");
    if (automationRun) {
      automationRun.replaceChildren();
      [
        ["workflow", currentLanguage === "hr" ? "Uključena pravila" : "Enabled rules", `${dashboardData.automation?.enabledRules || 0}/${dashboardData.automation?.totalRules || 0}`],
        ["play", currentLanguage === "hr" ? "Ukupno pokretanja" : "Total runs", String(dashboardData.automation?.runCount || 0)],
        ["clock-3", currentLanguage === "hr" ? "Posljednje pokretanje" : "Last run", formatDateTime(dashboardData.automation?.lastRunAt)],
      ].forEach(([iconName, label, value]) => {
        const step = document.createElement("div");
        step.className = "run-step done";
        const iconWrap = document.createElement("span");
        const icon = document.createElement("i");
        icon.dataset.lucide = iconName;
        iconWrap.append(icon);
        const copy = document.createElement("div");
        const title = document.createElement("strong");
        title.textContent = label;
        const detail = document.createElement("small");
        detail.textContent = value;
        copy.append(title, detail);
        step.append(iconWrap, copy);
        automationRun.append(step);
      });
    }

    const pipeline = document.querySelector("#dashboard-pipeline");
    if (pipeline) {
      pipeline.replaceChildren();
      const entries = [
        ["collected", currentLanguage === "hr" ? "Prikupljeno" : "Collected", "new", "", "source"],
        ["inProgress", currentLanguage === "hr" ? "U izradi" : "In progress", "working", "draft", "all"],
        ["inReview", currentLanguage === "hr" ? "Za pregled" : "In review", "review", "in_review", "all"],
        ["scheduled", currentLanguage === "hr" ? "Zakazano" : "Scheduled", "ready", "scheduled", "all"],
      ];
      entries.forEach(([key, label, dot, status, kind]) => {
        const column = document.createElement("button");
        column.type = "button";
        column.className = "pipeline-stage";
        column.dataset.pipelineStatus = status;
        column.dataset.pipelineKind = kind;
        column.title = currentLanguage === "hr" ? `Prikaži: ${label}` : `Show: ${label}`;
        column.setAttribute("aria-label", column.title);
        const caption = document.createElement("span");
        caption.className = "pipeline-label";
        const marker = document.createElement("i");
        marker.className = `dot ${dot}`;
        caption.append(marker, document.createTextNode(label));
        const count = document.createElement("strong");
        count.textContent = String(dashboardData.pipeline?.[key] || 0);
        const hint = document.createElement("small");
        hint.className = "pipeline-open-hint";
        hint.textContent = currentLanguage === "hr" ? "Otvori stavke" : "Open items";
        column.append(caption, count, hint);
        pipeline.append(column);
      });
    }

    const today = document.querySelector("#dashboard-today");
    if (today) {
      today.replaceChildren();
      (dashboardData.today || []).forEach((item) => {
        const row = document.createElement("button");
        row.type = "button";
        row.className = "timeline-item";
        row.dataset.dashboardCalendarId = item.id;
        const time = document.createElement("strong");
        time.textContent = new Intl.DateTimeFormat(currentLanguage === "hr" ? "hr-HR" : "en-GB", { hour: "2-digit", minute: "2-digit" }).format(new Date(item.scheduledFor));
        const copy = document.createElement("span");
        copy.className = "timeline-copy";
        const title = document.createElement("strong");
        title.textContent = item.title;
        const meta = document.createElement("span");
        meta.className = "timeline-meta";
        const channel = document.createElement("span");
        channel.className = "timeline-channel-icon";
        const channelName = item.channel ? `${item.channel.charAt(0).toUpperCase()}${item.channel.slice(1)}` : "—";
        channel.title = channelName;
        channel.setAttribute("aria-label", channelName);
        const channelIcon = document.createElement("i");
        channelIcon.dataset.lucide = calendarChannelIcon(item.channel);
        channel.append(channelIcon);
        const status = document.createElement("span");
        status.className = `status-pill ${contentStatusClass(item.status)}`;
        status.textContent = contentStatusLabel(item.status);
        meta.append(channel, status);
        copy.append(title, meta);
        row.append(time, copy);
        today.append(row);
      });
      if (!(dashboardData.today || []).length) {
        const empty = document.createElement("p");
        empty.className = "content-empty";
        empty.textContent = currentLanguage === "hr" ? "Danas nema zakazanih objava." : "Nothing is scheduled today.";
        today.append(empty);
      }
    }
    const todaySummary = document.querySelector(".schedule-panel .panel-head p");
    if (todaySummary) {
      const channels = new Set((dashboardData.today || []).map((item) => item.channel));
      todaySummary.textContent = `${(dashboardData.today || []).length} ${currentLanguage === "hr" ? "stavki" : "items"} · ${channels.size} ${currentLanguage === "hr" ? "kanala" : "channels"}`;
    }

    const channelGrid = document.querySelector("#dashboard-channel-health");
    if (channelGrid) {
      channelGrid.replaceChildren();
      (dashboardData.channels || []).forEach((channel) => {
        const card = document.createElement("button");
        card.type = "button";
        const providerClass = channel.provider === "x" ? "x-network" : String(channel.provider || "web").replace(/[^a-z0-9-]/gi, "").toLowerCase();
        const isHealthy = ["connected", "active", "ready", "ok"].includes(String(channel.status || "").toLowerCase());
        card.className = `channel-card ${isHealthy ? "connected" : ""}`;
        card.dataset.dashboardChannel = channel.provider;
        const iconWrap = document.createElement("span");
        iconWrap.className = `channel-card-icon mini-channel ${providerClass}`;
        const icon = document.createElement("i");
        icon.dataset.lucide = calendarChannelIcon(channel.provider);
        iconWrap.append(icon);
        const copy = document.createElement("span");
        copy.className = "channel-card-copy";
        const name = document.createElement("strong");
        name.textContent = channel.displayName || channel.provider;
        const detail = document.createElement("small");
        detail.textContent = `${channel.accountHandle || channel.source} · ${channel.status}`;
        copy.append(name, detail);
        const healthIndicator = document.createElement("span");
        healthIndicator.className = isHealthy ? "health-ok" : "health-warn";
        healthIndicator.title = channel.status || (currentLanguage === "hr" ? "Nepoznat status" : "Unknown status");
        card.append(iconWrap, copy, healthIndicator);
        channelGrid.append(card);
      });
      if (!(dashboardData.channels || []).length) {
        const empty = document.createElement("p");
        empty.className = "content-empty";
        empty.textContent = currentLanguage === "hr" ? "Nema povezanih kanala." : "No connected channels.";
        channelGrid.append(empty);
      }
    }
    renderDashboardContent();
    renderSessionIdentity();
    refreshIcons();
  }

  async function loadDashboard() {
    dashboardData = await apiRequest(`/projects/${projectID}/dashboard`);
    renderDashboard();
  }

  function hydrateProfile() {
    if (!projectProfile) return;
    const values = {
      "#profile-project-name": projectProfile.projectName,
      "#profile-company-name": projectProfile.companyName,
      "#profile-company-description": projectProfile.companyDescription,
      "#profile-website-url": projectProfile.websiteUrl,
      "#profile-industry": projectProfile.industry,
      "#profile-primary-language": projectProfile.primaryLanguage,
      "#profile-social-posts": projectProfile.socialPostsPerWeek,
      "#profile-newsletter-cadence": projectProfile.newsletterCadence,
      "#profile-timezone": projectProfile.timezone,
      "#website-signup-headline": projectProfile.signupHeadline,
      "#website-signup-copy": projectProfile.signupCopy,
    };
    Object.entries(values).forEach(([selector, value]) => {
      const input = document.querySelector(selector);
      if (!input || value == null) return;
      if (input.tagName === "SELECT") ensureSelectValue(input, value, value);
      else input.value = value;
    });
    setText("#setup-completion", `${setupCompletionPercent()}%`);
    setText("#signup-preview-headline", projectProfile.signupHeadline);
    setText("#signup-preview-copy", projectProfile.signupCopy);
    renderEntitlementBranding();
    renderSettingsStrategy();
  }

  function profilePayload(setupCompleted = projectProfile?.setupCompleted || false) {
    return {
      projectName: document.querySelector("#profile-project-name")?.value.trim() || projectAccess?.projectName || (currentLanguage === "hr" ? "Novi projekt" : "New project"),
      companyName: document.querySelector("#profile-company-name")?.value.trim() || "",
      companyDescription: document.querySelector("#profile-company-description")?.value.trim() || "",
      websiteUrl: document.querySelector("#profile-website-url")?.value.trim() || "",
      industry: document.querySelector("#profile-industry")?.value || "",
      primaryLanguage: document.querySelector("#profile-primary-language")?.value || currentLanguage || "hr",
      timezone: document.querySelector("#profile-timezone")?.value.trim() || projectProfile?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || "Europe/Zagreb",
      socialPostsPerWeek: Number(document.querySelector("#profile-social-posts")?.value ?? projectProfile?.socialPostsPerWeek ?? 5),
      newsletterCadence: document.querySelector("#profile-newsletter-cadence")?.value || projectProfile?.newsletterCadence || "weekly",
      signupHeadline: document.querySelector("#website-signup-headline")?.value.trim() ?? projectProfile?.signupHeadline ?? (currentLanguage === "hr" ? "Budite u toku." : "Stay in the loop."),
      signupCopy: document.querySelector("#website-signup-copy")?.value.trim() ?? projectProfile?.signupCopy ?? (currentLanguage === "hr" ? "Najvažnije priče jednom tjedno." : "The most important stories once a week."),
      setupCompleted,
    };
  }

  async function loadProfile() {
    projectProfile = await apiRequest(`/projects/${projectID}/profile`);
    hydrateProfile();
  }

  async function saveProfile(setupCompleted = projectProfile?.setupCompleted || false) {
    if (!projectID || !projectRoleAllows("owner", "lead")) return null;
    window.clearTimeout(profileSaveTimer);
    const targetProjectID = projectID;
    const requestVersion = ++profileChangeVersion;
    const payload = profilePayload(setupCompleted);
    const queuedSave = profileSaveQueue.then(async () => {
      const saved = await apiRequest(`/projects/${targetProjectID}/profile`, {
        method: "PUT",
        body: JSON.stringify(payload),
      });
      if (requestVersion === profileChangeVersion && targetProjectID === projectID) {
        projectProfile = saved;
        if (projectAccess) projectAccess.projectName = projectProfile.projectName;
        renderSessionIdentity();
        hydrateProfile();
        await loadDashboard();
        if (requestVersion === profileChangeVersion && targetProjectID === projectID) {
          showToast(setupCompleted ? "setup" : "saved");
        }
      }
      return saved;
    });
    profileSaveQueue = queuedSave.then(() => undefined, () => undefined);
    try {
      return await queuedSave;
    } catch (error) {
      if (requestVersion === profileChangeVersion && targetProjectID === projectID) {
        showDomainError(currentLanguage === "hr" ? "Spremanje profila" : "Saving profile", error);
      } else {
        console.warn("Ignored stale profile save error", error);
      }
      return null;
    }
  }

  function scheduleProfileSave() {
    if (!hydrated || !projectID || !projectRoleAllows("owner", "lead")) return;
    profileChangeVersion += 1;
    window.clearTimeout(profileSaveTimer);
    profileSaveTimer = window.setTimeout(() => saveProfile(), 700);
  }

  function defaultAutomation(ruleKey, enabled) {
    const channelKeys = new Set(["linkedin", "instagram", "facebook", "youtube", "x", "reddit", "pinterest", "threads", "telegram", "blog"]);
    const names = {
      master: "Glavna automatizacija", bot_event: "Događaj iz bota", calendar_gap: "Popuni praznine u kalendaru",
      weekly_newsletter: "Tjedni newsletter", newsletter: "Newsletter", linkedin: "LinkedIn", instagram: "Instagram",
      facebook: "Facebook", youtube: "YouTube", x: "X", reddit: "Reddit", pinterest: "Pinterest", threads: "Threads", telegram: "Telegram", blog: "Blog",
    };
    return {
      ruleKey,
      name: names[ruleKey] || ruleKey.replaceAll("_", " "),
      description: currentLanguage === "hr" ? "Pravilo projekta povezano s aktivnim radnim tijekom." : "Project rule connected to the active workflow.",
      kind: ruleKey === "master" ? "master" : ruleKey === "bot_event" ? "bot_event" : ruleKey === "calendar_gap" ? "calendar_gap" : ruleKey === "weekly_newsletter" || ruleKey === "newsletter" ? "newsletter" : channelKeys.has(ruleKey) ? "channel" : "custom",
      channel: channelKeys.has(ruleKey) ? ruleKey : ruleKey === "weekly_newsletter" || ruleKey === "newsletter" ? "newsletter" : "",
      enabled,
      reviewPolicy: ruleKey === "reddit" ? "always" : "conditional",
      scheduleRule: ruleKey === "weekly_newsletter" || ruleKey === "newsletter" ? "FREQ=WEEKLY;BYDAY=FR;BYHOUR=10" : "",
      configuration: { source: "millena-ui" },
    };
  }

  function automationPayload(rule, changes = {}) {
    return {
      ruleKey: changes.ruleKey ?? rule.ruleKey,
      name: changes.name ?? rule.name,
      description: changes.description ?? rule.description ?? "",
      kind: changes.kind ?? rule.kind,
      channel: changes.channel ?? rule.channel ?? "",
      enabled: changes.enabled ?? Boolean(rule.enabled),
      reviewPolicy: changes.reviewPolicy ?? rule.reviewPolicy ?? "always",
      scheduleRule: changes.scheduleRule ?? rule.scheduleRule ?? "",
      configuration: changes.configuration ?? rule.configuration ?? {},
    };
  }

  function syncAutomationInputs() {
    document.querySelectorAll("input[data-automation-rule-key]").forEach((input) => {
      const rule = automationRules.find((candidate) => candidate.ruleKey === input.dataset.automationRuleKey);
      input.checked = Boolean(rule?.enabled);
    });
  }

  function renderAutomations() {
    syncAutomationInputs();
    const grid = document.querySelector("#automation-rules");
    if (!grid) return;
    grid.replaceChildren();
    automationRules.forEach((rule) => {
      const card = document.createElement("article");
      card.className = "rule-card panel";
      card.dataset.automationRuleKey = rule.ruleKey;
      const head = document.createElement("div");
      head.className = "rule-head";
      const iconWrap = document.createElement("span");
      iconWrap.className = `mini-channel ${rule.channel || rule.kind}`;
      const icon = document.createElement("i");
      icon.dataset.lucide = rule.kind === "newsletter" ? "mail" : rule.kind === "calendar_gap" ? "calendar-range" : rule.kind === "bot_event" ? "message-circle" : rule.kind === "master" ? "workflow" : calendarChannelIcon(rule.channel);
      iconWrap.append(icon);
      const toggleLabel = document.createElement("label");
      toggleLabel.className = "switch";
      const input = document.createElement("input");
      input.type = "checkbox";
      input.checked = rule.enabled;
      input.dataset.automationRuleKey = rule.ruleKey;
      input.setAttribute("aria-label", rule.name);
      toggleLabel.append(input, document.createElement("span"));
      head.append(iconWrap, toggleLabel);
      const name = document.createElement("strong");
      name.textContent = rule.name;
      const description = document.createElement("p");
      description.textContent = rule.description || (currentLanguage === "hr" ? "Bez opisa" : "No description");
      const footer = document.createElement("footer");
      const history = document.createElement("span");
      history.textContent = `${currentLanguage === "hr" ? "Pokretanja" : "Runs"}: ${rule.runCount || 0} · ${formatDateTime(rule.lastRunAt)}`;
      const edit = document.createElement("button");
      edit.type = "button";
      edit.dataset.automationEdit = rule.id;
      edit.setAttribute("aria-label", currentLanguage === "hr" ? "Uredi pravilo" : "Edit rule");
      const editIcon = document.createElement("i");
      editIcon.dataset.lucide = "more-horizontal";
      edit.append(editIcon);
      footer.append(history, edit);
      card.append(head, name, description, footer);
      grid.append(card);
    });
    renderSocialAutomation();
    if (hydrated) applyRoleUI();
    refreshIcons();
  }

  async function loadAutomations() {
    automationRules = await apiRequest(`/projects/${projectID}/automations`);
    renderAutomations();
  }

  async function toggleAutomation(input) {
    const ruleKey = input.dataset.automationRuleKey;
    const desired = input.checked;
    const matching = [...document.querySelectorAll(`input[data-automation-rule-key="${ruleKey}"]`)];
    matching.forEach((control) => { control.disabled = true; });
    try {
      const existing = automationRules.find((rule) => rule.ruleKey === ruleKey);
      const updated = await apiRequest(existing
        ? `/projects/${projectID}/automations/${existing.id}`
        : `/projects/${projectID}/automations`, {
        method: existing ? "PUT" : "POST",
        body: JSON.stringify(existing ? automationPayload(existing, { enabled: desired }) : defaultAutomation(ruleKey, desired)),
      });
      const index = automationRules.findIndex((rule) => rule.id === updated.id);
      if (index >= 0) automationRules[index] = updated;
      else automationRules.push(updated);
      renderAutomations();
      await loadDashboard();
      showToast("saved");
    } catch (error) {
      matching.forEach((control) => { control.checked = !desired; });
      showDomainError(currentLanguage === "hr" ? "Promjena automatizacije" : "Updating automation", error);
    } finally {
      matching.forEach((control) => { control.disabled = false; });
    }
  }

  function openAutomationModal(rule = null) {
    const source = rule || defaultAutomation(`custom_${Date.now()}`, true);
    const configuration = source.configuration || {};
    document.querySelector("#automation-id").value = rule?.id || "";
    document.querySelector("#automation-name").value = rule?.name || "";
    document.querySelector("#automation-description").value = rule?.description || "";
    ensureSelectValue(document.querySelector("#automation-kind"), source.kind, source.kind);
    ensureSelectValue(document.querySelector("#automation-channel"), source.channel, source.channel);
    ensureSelectValue(document.querySelector("#automation-review"), source.reviewPolicy, source.reviewPolicy);
    document.querySelector("#automation-schedule").value = source.scheduleRule || "";
    document.querySelector("#automation-enabled").checked = Boolean(source.enabled);
    document.querySelector("#automation-content-kind").value = ["source", "social", "blog", "newsletter", "press_release", "case_study", "event"].includes(configuration.contentKind) ? configuration.contentKind : "";
    document.querySelector("#automation-formats").value = Array.isArray(configuration.formats) ? configuration.formats.join(", ") : "";
    document.querySelector("#automation-target-channels").value = Array.isArray(configuration.channels) ? configuration.channels.join(", ") : "";
    document.querySelector("#automation-gap-days").value = Number.isInteger(configuration.gapDays) ? String(configuration.gapDays) : "";
    document.querySelector("#automation-fact-check").value = typeof configuration.factCheck === "boolean" ? String(configuration.factCheck) : "";
    document.querySelector("#automation-respect-forbidden").value = typeof configuration.respectForbiddenTopics === "boolean" ? String(configuration.respectForbiddenTopics) : "";
    document.querySelector("#automation-cadence").value = ["off", "weekly", "biweekly", "monthly"].includes(configuration.cadence) ? configuration.cadence : "";
    document.querySelector("#automation-hour").value = Number.isInteger(configuration.hour) ? String(configuration.hour) : "";
    document.querySelector("#automation-minute").value = Number.isInteger(configuration.minute) ? String(configuration.minute) : "";
    syncAutomationScheduleFields();
    document.querySelector("#automation-delete").hidden = !rule;
    document.querySelector("#automation-run").hidden = !rule;
    openDomainModal("automation-modal");
    document.querySelector("#automation-name")?.focus();
  }

  function syncAutomationScheduleFields() {
    const scheduleRule = document.querySelector("#automation-schedule")?.value.trim() || "";
    const explicitSchedule = Boolean(scheduleRule);
    const gapSchedule = /^gap:/i.test(scheduleRule);
    const scheduleCopy = currentLanguage === "hr"
      ? "Eksplicitni raspored ima prednost; ovu vrijednost uklonite iz rasporeda da biste je ponovno koristili."
      : "An explicit schedule takes precedence; remove it to use this value again.";
    setControlLock(["#automation-cadence"], "schedule", explicitSchedule, scheduleCopy);
    setControlLock(["#automation-hour", "#automation-minute"], "schedule", explicitSchedule && !gapSchedule, scheduleCopy);
  }

  function automationCSV(selector, allowed, label) {
    const raw = document.querySelector(selector)?.value || "";
    const values = [...new Set(raw.split(",").map((value) => value.trim().toLocaleLowerCase("en")).filter(Boolean))];
    const invalid = values.filter((value) => !allowed.has(value));
    if (invalid.length) throw new Error(`${label}: ${invalid.join(", ")}`);
    return values;
  }

  function automationInteger(selector, min, max, label) {
    const raw = document.querySelector(selector)?.value.trim() || "";
    if (!raw) return null;
    const value = Number(raw);
    if (!Number.isInteger(value) || value < min || value > max) throw new Error(`${label}: ${min}–${max}`);
    return value;
  }

  function automationConfigurationFromModal(existing = null) {
    const configuration = { ...(existing?.configuration || {}) };
    const scheduleRule = document.querySelector("#automation-schedule")?.value.trim() || "";
    const gapSchedule = /^gap:/i.test(scheduleRule);
    const contentKind = document.querySelector("#automation-content-kind")?.value || "";
    if (contentKind) configuration.contentKind = contentKind;
    else delete configuration.contentKind;

    const formatLabel = currentLanguage === "hr" ? "Nepoznat format" : "Unknown format";
    const channelLabel = currentLanguage === "hr" ? "Nepoznat kanal" : "Unknown channel";
    const formats = automationCSV("#automation-formats", new Set(["source", "social", "blog", "newsletter", "press_release", "case_study", "event"]), formatLabel);
    const channels = automationCSV("#automation-target-channels", new Set(["linkedin", "instagram", "facebook", "youtube", "x", "reddit", "pinterest", "threads", "telegram", "blog", "newsletter", "website", "media"]), channelLabel);
    if (formats.length) configuration.formats = formats;
    else delete configuration.formats;
    if (channels.length) configuration.channels = channels;
    else delete configuration.channels;

    const gapDays = automationInteger("#automation-gap-days", 1, 365, currentLanguage === "hr" ? "Praznina u danima" : "Gap in days");
    const hour = scheduleRule && !gapSchedule ? null : automationInteger("#automation-hour", 0, 23, currentLanguage === "hr" ? "Sat" : "Hour");
    const minute = scheduleRule && !gapSchedule ? null : automationInteger("#automation-minute", 0, 59, currentLanguage === "hr" ? "Minuta" : "Minute");
    if (gapDays == null) delete configuration.gapDays; else configuration.gapDays = gapDays;
    if (hour == null) delete configuration.hour; else configuration.hour = hour;
    if (minute == null) delete configuration.minute; else configuration.minute = minute;

    const factCheck = document.querySelector("#automation-fact-check")?.value || "";
    if (!factCheck) delete configuration.factCheck;
    else configuration.factCheck = factCheck === "true";
    const respectForbiddenTopics = document.querySelector("#automation-respect-forbidden")?.value || "";
    if (!respectForbiddenTopics) delete configuration.respectForbiddenTopics;
    else configuration.respectForbiddenTopics = respectForbiddenTopics === "true";
    const cadence = scheduleRule ? "" : document.querySelector("#automation-cadence")?.value || "";
    if (!cadence) delete configuration.cadence;
    else configuration.cadence = cadence;
    return configuration;
  }

  function automationModalPayload(existing = null) {
    const name = document.querySelector("#automation-name").value.trim();
    return automationPayload(existing || defaultAutomation(`custom_${Date.now()}`, true), {
      ruleKey: existing?.ruleKey || name.toLocaleLowerCase("en").normalize("NFD").replace(/[\u0300-\u036f]/g, "").replace(/[^a-z0-9]+/g, "_").replace(/^_|_$/g, "").slice(0, 70) || `custom_${Date.now()}`,
      name,
      description: document.querySelector("#automation-description").value.trim(),
      kind: document.querySelector("#automation-kind").value,
      channel: document.querySelector("#automation-channel").value,
      reviewPolicy: document.querySelector("#automation-review").value,
      scheduleRule: document.querySelector("#automation-schedule").value.trim(),
      enabled: document.querySelector("#automation-enabled").checked,
      configuration: automationConfigurationFromModal(existing),
    });
  }

  async function saveAutomation() {
    const id = document.querySelector("#automation-id").value;
    const existing = automationRules.find((rule) => rule.id === id) || null;
    const button = document.querySelector("#automation-save");
    button.disabled = true;
    try {
      await apiRequest(id ? `/projects/${projectID}/automations/${id}` : `/projects/${projectID}/automations`, {
        method: id ? "PUT" : "POST", body: JSON.stringify(automationModalPayload(existing)),
      });
      closeDomainModal("automation-modal");
      await Promise.all([loadAutomations(), loadDashboard()]);
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Spremanje automatizacije" : "Saving automation", error);
    } finally {
      button.disabled = false;
    }
  }

  async function deleteAutomation() {
    const id = document.querySelector("#automation-id").value;
    if (!id) return;
    const rule = automationRules.find((candidate) => candidate.id === id);
    const label = rule?.name || document.querySelector("#automation-name")?.value.trim() || (currentLanguage === "hr" ? "pravilo" : "rule");
    const confirmed = window.confirm(currentLanguage === "hr"
      ? `Obrisati automatizaciju „${label}”?`
      : `Delete automation “${label}”?`);
    if (!confirmed) return;
    try {
      await apiRequest(`/projects/${projectID}/automations/${id}`, { method: "DELETE" });
      closeDomainModal("automation-modal");
      await Promise.all([loadAutomations(), loadDashboard()]);
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Brisanje automatizacije" : "Deleting automation", error);
    }
  }

  async function runAutomation() {
    const id = document.querySelector("#automation-id").value;
    if (!id) return;
    const button = document.querySelector("#automation-run");
    button.disabled = true;
    try {
      const result = await apiRequest(`/projects/${projectID}/automations/${id}/run`, { method: "POST" });
      closeDomainModal("automation-modal");
      await Promise.all([loadAutomations(), loadDashboard(), loadContent(), loadCalendar()]);
      openActionModal(result.effectTitle, `${currentLanguage === "hr" ? "Stvarni učinak automatizacije" : "Automation effect"}: ${result.effectType} · ${formatDateTime(result.runAt)}`, currentLanguage === "hr" ? "Automatizacija je izvršena" : "Automation completed");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Pokretanje automatizacije" : "Running automation", error);
    } finally {
      button.disabled = false;
    }
  }

  function populateAudienceListSelects() {
    ["#contact-list", "#newsletter-list"].forEach((selector) => {
      const select = document.querySelector(selector);
      if (!select) return;
      const selected = select.value;
      select.replaceChildren(new Option(currentLanguage === "hr" ? "Odaberite listu" : "Choose a list", ""));
      audienceLists.forEach((list) => {
        const option = new Option(`${list.name} (${list.activeCount}/${list.contactCount})`, list.id);
        select.add(option);
      });
      select.add(new Option(currentLanguage === "hr" ? "+ Nova lista…" : "+ New list…", "__new__"));
      if ([...select.options].some((option) => option.value === selected)) select.value = selected;
      else if (audienceLists.length) select.value = audienceLists.find((list) => list.isDefault)?.id || audienceLists[0].id;
    });
    const filter = document.querySelector("#audience-list-filter");
    if (filter) {
      const selected = activeAudienceListID || filter.value;
      filter.replaceChildren(new Option(currentLanguage === "hr" ? "Svi kontakti" : "All contacts", ""));
      audienceLists.forEach((list) => filter.add(new Option(`${list.isDefault ? "★ " : ""}${list.name} (${list.contactCount})`, list.id)));
      filter.value = audienceLists.some((list) => list.id === selected) ? selected : "";
      activeAudienceListID = filter.value;
    }
    renderNewsletterEstimate();
  }

  function renderAudience() {
    const stats = audienceStats || { total: 0, active: 0, website: 0, pending: 0 };
    setText("#audience-total", stats.total);
    setText("#audience-active", stats.active);
    setText("#audience-website", stats.website);
    setText("#audience-pending", stats.pending);
    document.querySelectorAll("[data-audience-count]").forEach((node) => { node.textContent = String(stats.active || 0); });
    setText("#signup-preview-headline", projectProfile?.signupHeadline || (currentLanguage === "hr" ? "Budite u toku." : "Stay in the loop."));
    setText("#signup-preview-copy", projectProfile?.signupCopy || (currentLanguage === "hr" ? "Najvažnije priče jednom tjedno." : "The most important stories once a week."));
    populateAudienceListSelects();
    const list = document.querySelector("#audience-contacts");
    if (!list) return;
    list.replaceChildren();
    if (!audienceContacts.length) {
      const empty = document.createElement("div");
      empty.className = "content-loading";
      empty.textContent = currentLanguage === "hr" ? "Nema kontakata za odabranu pretragu." : "No contacts match this search.";
      list.append(empty);
      return;
    }
    audienceContacts.forEach((contact) => {
      const row = document.createElement("div");
      row.className = "contact-row";
      row.dataset.contactId = contact.id;
      const identity = document.createElement("span");
      const avatar = document.createElement("span");
      avatar.className = "avatar small";
      avatar.textContent = `${contact.firstName?.[0] || ""}${contact.lastName?.[0] || contact.email?.[0] || ""}`.toUpperCase();
      const copy = document.createElement("span");
      const name = document.createElement("strong");
      name.textContent = `${contact.firstName || ""} ${contact.lastName || ""}`.trim() || contact.email;
      const email = document.createElement("small");
      email.textContent = contact.email;
      copy.append(name, email);
      identity.append(avatar, copy);
      const source = document.createElement("span");
      source.textContent = contact.source;
      const created = document.createElement("span");
      created.textContent = new Intl.DateTimeFormat(currentLanguage === "hr" ? "hr-HR" : "en-GB", { day: "2-digit", month: "short" }).format(new Date(contact.createdAt));
      const status = document.createElement("span");
      status.className = `status-pill ${contact.status === "active" ? "auto" : contact.status === "pending" ? "review" : "working"}`;
      status.textContent = contact.status;
      const edit = document.createElement("button");
      edit.type = "button";
      edit.dataset.contactEdit = contact.id;
      edit.setAttribute("aria-label", currentLanguage === "hr" ? "Uredi kontakt" : "Edit contact");
      const icon = document.createElement("i");
      icon.dataset.lucide = "pencil";
      edit.append(icon);
      row.append(identity, source, created, status, edit);
      list.append(row);
    });
    refreshIcons();
  }

  async function loadAudience(search = document.querySelector("#audience-search")?.value.trim() || "") {
    const requestToken = ++audienceLoadToken;
    const params = new URLSearchParams();
    if (search) params.set("search", search);
    if (activeAudienceListID) params.set("listId", activeAudienceListID);
    const query = params.size ? `?${params.toString()}` : "";
    let result;
    try {
      result = await Promise.all([
        apiRequest(`/projects/${projectID}/audience/lists`),
        apiRequest(`/projects/${projectID}/audience/contacts${query}`),
      ]);
    } catch (error) {
      if (requestToken !== audienceLoadToken) return null;
      throw error;
    }
    if (requestToken !== audienceLoadToken) return null;
    const [lists, contacts] = result;
    audienceLists = lists;
    audienceContacts = contacts.items || [];
    audienceStats = contacts.stats || null;
    renderAudience();
    renderWebsiteIntegration();
    return { lists, contacts };
  }

  function openAudienceListModal(list = null) {
    document.querySelector("#audience-list-id").value = list?.id || "";
    document.querySelector("#audience-list-name").value = list?.name || "";
    document.querySelector("#audience-list-description").value = list?.description || "";
    document.querySelector("#audience-list-default").checked = Boolean(list?.isDefault || audienceLists.length === 0);
    const canManage = projectRoleAllows("owner", "lead");
    const canCreate = projectRoleAllows("owner", "lead", "editor");
    document.querySelector("#audience-list-delete").hidden = !canManage || !list || list.isDefault || list.contactCount > 0;
    setControlLock(["#audience-list-save"], "role", list ? !canManage : !canCreate,
      currentLanguage === "hr" ? "Vaša uloga nema dopuštenje za ovu promjenu liste." : "Your role cannot make this list change.");
    const title = document.querySelector("#audience-list-modal-title");
    if (title) title.textContent = list ? (currentLanguage === "hr" ? "Uredi newsletter listu" : "Edit newsletter list") : (currentLanguage === "hr" ? "Nova newsletter lista" : "New newsletter list");
    openDomainModal("audience-list-modal");
    document.querySelector("#audience-list-name")?.focus();
  }

  async function saveAudienceList() {
    const id = document.querySelector("#audience-list-id").value;
    const name = document.querySelector("#audience-list-name").value.trim();
    const button = document.querySelector("#audience-list-save");
    if (name.length < 2) {
      document.querySelector("#audience-list-name")?.reportValidity();
      return;
    }
    button.disabled = true;
    try {
      const list = await apiRequest(id ? `/projects/${projectID}/audience/lists/${id}` : `/projects/${projectID}/audience/lists`, {
        method: id ? "PUT" : "POST",
        body: JSON.stringify({
          name,
          description: document.querySelector("#audience-list-description").value.trim(),
          isDefault: document.querySelector("#audience-list-default").checked,
        }),
      });
      activeAudienceListID = list.id;
      closeDomainModal("audience-list-modal");
      await loadAudience();
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Spremanje liste" : "Saving list", error);
    } finally {
      button.disabled = false;
    }
  }

  async function deleteAudienceList() {
    const id = document.querySelector("#audience-list-id").value;
    if (!id) return;
    try {
      await apiRequest(`/projects/${projectID}/audience/lists/${id}`, { method: "DELETE" });
      activeAudienceListID = "";
      closeDomainModal("audience-list-modal");
      await loadAudience();
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Brisanje liste" : "Deleting list", error);
    }
  }

  async function createAudienceList(select) {
    const name = window.prompt(currentLanguage === "hr" ? "Naziv nove liste" : "New list name", currentLanguage === "hr" ? "Newsletter" : "Newsletter");
    if (!name?.trim()) {
      select.value = audienceLists.find((list) => list.isDefault)?.id || audienceLists[0]?.id || "";
      return;
    }
    try {
      const list = await apiRequest(`/projects/${projectID}/audience/lists`, {
        method: "POST",
        body: JSON.stringify({ name: name.trim(), description: currentLanguage === "hr" ? "Lista izrađena u aplikaciji" : "List created in the app", isDefault: audienceLists.length === 0 }),
      });
      await loadAudience();
      ["#contact-list", "#newsletter-list"].forEach((selector) => {
        const target = document.querySelector(selector);
        if (target) target.value = list.id;
      });
      renderNewsletterEstimate();
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Izrada liste" : "Creating list", error);
    }
  }

  function openContactEditor(contact = null) {
    document.querySelector("#contact-id").value = contact?.id || "";
    document.querySelector("#contact-first-name").value = contact?.firstName || "";
    document.querySelector("#contact-last-name").value = contact?.lastName || "";
    document.querySelector("#contact-email").value = contact?.email || "";
    ensureSelectValue(document.querySelector("#contact-list"), contact?.listId || audienceLists.find((list) => list.isDefault)?.id || audienceLists[0]?.id || "", contact?.listName || "");
    ensureSelectValue(document.querySelector("#contact-status"), contact?.status || "pending", contact?.status || "pending");
    document.querySelector("#contact-consent").checked = Boolean(contact?.consent);
    document.querySelector("#contact-delete").hidden = !contact || !projectRoleAllows("owner", "lead");
    const title = document.querySelector("#contact-modal-title");
    if (title) title.textContent = contact ? (currentLanguage === "hr" ? "Uredi kontakt" : "Edit contact") : (currentLanguage === "hr" ? "Dodaj kontakt" : "Add contact");
    openDomainModal("contact-modal");
    document.querySelector("#contact-first-name")?.focus();
  }

  function contactPayload(existing = null) {
    const listID = document.querySelector("#contact-list").value;
    return {
      listId: listID && listID !== "__new__" ? listID : null,
      firstName: document.querySelector("#contact-first-name").value.trim(),
      lastName: document.querySelector("#contact-last-name").value.trim(),
      email: document.querySelector("#contact-email").value.trim(),
      source: existing?.source || "manual",
      status: document.querySelector("#contact-status").value,
      consent: document.querySelector("#contact-consent").checked,
      metadata: { ...(existing?.metadata || {}), editor: "audience-modal" },
    };
  }

  async function saveContact() {
    const id = document.querySelector("#contact-id").value;
    const existing = audienceContacts.find((contact) => contact.id === id) || null;
    const button = document.querySelector("#contact-save");
    button.disabled = true;
    try {
      await apiRequest(id ? `/projects/${projectID}/audience/contacts/${id}` : `/projects/${projectID}/audience/contacts`, {
        method: id ? "PUT" : "POST", body: JSON.stringify(contactPayload(existing)),
      });
      closeDomainModal("contact-modal");
      await Promise.all([loadAudience(), loadDashboard()]);
      showToast("contact");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Spremanje kontakta" : "Saving contact", error);
    } finally {
      button.disabled = false;
    }
  }

  async function deleteContact() {
    const id = document.querySelector("#contact-id").value;
    if (!id) return;
    const contact = audienceContacts.find((candidate) => candidate.id === id);
    const label = [contact?.firstName, contact?.lastName].filter(Boolean).join(" ") || contact?.email || document.querySelector("#contact-email")?.value.trim() || (currentLanguage === "hr" ? "kontakt" : "contact");
    const confirmed = window.confirm(currentLanguage === "hr"
      ? `Obrisati kontakt „${label}”?`
      : `Delete contact “${label}”?`);
    if (!confirmed) return;
    try {
      await apiRequest(`/projects/${projectID}/audience/contacts/${id}`, { method: "DELETE" });
      closeDomainModal("contact-modal");
      await Promise.all([loadAudience(), loadDashboard()]);
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Brisanje kontakta" : "Deleting contact", error);
    }
  }

  async function importAudienceCSV(input) {
    const file = input.files?.[0];
    if (!file) return;
    const form = new FormData();
    form.append("file", file);
    const listID = activeAudienceListID || document.querySelector("#contact-list")?.value || document.querySelector("#newsletter-list")?.value;
    if (listID && listID !== "__new__") form.append("listId", listID);
    try {
      const result = await apiRequest(`/projects/${projectID}/audience/import/csv`, { method: "POST", body: form });
      await Promise.all([loadAudience(), loadDashboard()]);
      openActionModal(
        currentLanguage === "hr" ? `${result.imported} uvezeno · ${result.updated} ažurirano` : `${result.imported} imported · ${result.updated} updated`,
        result.errors?.length ? result.errors.join(" · ") : (currentLanguage === "hr" ? `${result.skipped} preskočeno.` : `${result.skipped} skipped.`),
        currentLanguage === "hr" ? "CSV uvoz je dovršen" : "CSV import complete",
      );
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "CSV uvoz" : "CSV import", error);
    } finally {
      input.value = "";
    }
  }

  function renderAssistant() {
    const thread = document.querySelector("#assistant-thread");
    if (!thread) return;
    thread.replaceChildren();
    thread.setAttribute("aria-busy", "false");
    if (!assistantMessages.length) {
      const empty = document.createElement("div");
      empty.className = "assistant-loading";
      empty.textContent = entitlement && !featureIncluded("aiAgents")
        ? (currentLanguage === "hr" ? "AI asistent nije uključen u aktivni paket." : "The AI assistant is not included in the active plan.")
        : (currentLanguage === "hr" ? "Razgovor je spreman. Pitajte za stanje projekta, kalendar ili zatražite nacrt." : "The conversation is ready. Ask about the workspace, calendar, or request a draft.");
      thread.append(empty);
    }
    assistantMessages.forEach((message) => {
      const row = document.createElement("article");
      row.className = `app-message ${message.role === "user" ? "user" : "bot"}`;
      if (message.role !== "user") {
        const image = document.createElement("img");
        image.src = "assets/millena-mark.png";
        image.alt = "Millena";
        row.append(image);
      }
      const bubble = document.createElement("div");
      const body = document.createElement("p");
      body.textContent = message.body;
      bubble.append(body);
      if (message.attachments?.length) {
        const attachments = document.createElement("div");
        attachments.className = "pending-assets message-assets";
        message.attachments.forEach((asset) => attachments.append(renderAssetChip(asset)));
        bubble.append(attachments);
      }
      if (message.role !== "user" && message.actionType && message.actionType !== "assistant.answer" && message.actionType !== "workspace.summary") {
        const result = document.createElement("div");
        result.className = "bot-result";
        const copy = document.createElement("span");
        const icon = document.createElement("i");
        icon.dataset.lucide = message.actionType.startsWith("content") ? "file-plus-2" : message.actionType.startsWith("automation") ? "workflow" : "calendar";
        const labels = document.createElement("span");
        const title = document.createElement("strong");
        title.textContent = message.actionType;
        const time = document.createElement("small");
        time.textContent = formatDateTime(message.createdAt);
        labels.append(title, time);
        copy.append(icon, labels);
        result.append(copy);
        if (message.actionEntityId) {
          const open = document.createElement("button");
          open.type = "button";
          open.dataset.assistantEntity = message.actionEntityId;
          open.dataset.assistantAction = message.actionType;
          open.textContent = currentLanguage === "hr" ? "Otvori" : "Open";
          result.append(open);
        }
        bubble.append(result);
      }
      row.append(bubble);
      thread.append(row);
    });
    thread.scrollTop = thread.scrollHeight;
    const context = document.querySelector("#assistant-context-status");
    if (context && assistantStatus) {
      context.textContent = `${assistantStatus.provider}${assistantStatus.model ? ` · ${assistantStatus.model}` : ""} · ${(assistantStatus.capabilities || []).length} ${currentLanguage === "hr" ? "aktivne mogućnosti" : "active capabilities"}`;
    }
    renderAssistantThreadList();
    renderAssistantAccess();
    renderAssistantChannelShortcuts();
    const activeThread = assistantThreads.find((candidate) => candidate.id === activeAssistantThreadID);
    const headerStatus = document.querySelector(".bot-conversation-panel > header small span");
    if (headerStatus) {
      const channel = activeThread?.channel || assistantChannel || "app";
      headerStatus.textContent = `${currentLanguage === "hr" ? "Aktivna" : "Active"} · ${channel} · ${activeThread?.title || (currentLanguage === "hr" ? "novi razgovor" : "new conversation")}`;
    }
    refreshIcons();
  }

  function renderAssistantThreadList() {
    const panel = document.querySelector(".bot-channel-panel");
    const access = panel?.querySelector(".bot-access");
    if (!panel || !access) return;
    let root = panel.querySelector("#assistant-thread-list");
    if (!root) {
      root = document.createElement("div");
      root.id = "assistant-thread-list";
      root.className = "assistant-thread-list";
      panel.insertBefore(root, access);
    }
    root.replaceChildren();
    const label = document.createElement("small");
    label.textContent = currentLanguage === "hr" ? "Spremljeni razgovori" : "Saved conversations";
    root.append(label);
    assistantThreads.forEach((item) => {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "bot-channel assistant-thread-choice";
      button.classList.toggle("active", item.id === activeAssistantThreadID);
      button.dataset.assistantThreadChoice = item.id;
      const iconWrap = document.createElement("span");
      iconWrap.className = "integration-icon";
      const icon = document.createElement("i");
      icon.dataset.lucide = item.channel === "telegram" ? "send" : item.channel === "whatsapp" ? "message-circle" : "messages-square";
      iconWrap.append(icon);
      const copy = document.createElement("span");
      const title = document.createElement("strong");
      title.textContent = item.title;
      const detail = document.createElement("small");
      detail.textContent = `${item.channel} · ${item.messageCount || 0} ${currentLanguage === "hr" ? "poruka" : "messages"}`;
      copy.append(title, detail);
      button.append(iconWrap, copy);
      button.addEventListener("click", async () => {
        activeAssistantThreadID = item.id;
        assistantChannel = item.channel;
        try {
          await loadAssistantMessages();
        } catch (error) {
          showDomainError(currentLanguage === "hr" ? "Učitavanje razgovora" : "Loading conversation", error);
        }
      });
      root.append(button);
    });
  }

  function renderAssistantAccess() {
    const access = document.querySelector(".bot-access");
    const people = access?.querySelector(":scope > div");
    const detail = access?.querySelector(":scope > small");
    if (!access || !people || !detail) return;
    people.replaceChildren();
    teamMembers.slice(0, 4).forEach((member) => {
      const avatar = document.createElement("span");
      avatar.className = "avatar small";
      avatar.title = `${member.displayName} · ${member.role}`;
      avatar.textContent = member.displayName.split(/\s+/).slice(0, 2).map((part) => part[0]).join("").toUpperCase();
      people.append(avatar);
    });
    const millena = document.createElement("img");
    millena.src = "assets/millena-mark.png";
    millena.alt = "Millena AI";
    people.append(millena);
    const add = document.createElement("button");
    add.type = "button";
    add.setAttribute("aria-label", currentLanguage === "hr" ? "Dodaj člana projekta" : "Add project member");
    const icon = document.createElement("i");
    icon.dataset.lucide = "plus";
    add.append(icon);
    add.addEventListener("click", () => {
      navigateTo("settings");
      openTeamModal();
    });
    people.append(add);
    detail.textContent = `${teamMembers.length} ${currentLanguage === "hr" ? "ljudi" : "people"} + Millena`;
  }

  function renderAssistantChannelShortcuts() {
    document.querySelectorAll(".bot-channel-panel > button.bot-channel:not([data-assistant-thread-choice])").forEach((button) => {
      const label = button.querySelector("strong")?.textContent.toLocaleLowerCase("en") || "";
      const provider = label.includes("whatsapp") ? "whatsapp" : "telegram";
      const connection = connectionForProvider(provider);
      const thread = assistantThreads.find((candidate) => candidate.channel === provider);
      const detail = button.querySelector("small");
      if (detail) {
        detail.textContent = connection?.accountHandle || connection?.displayName || (thread
          ? `${currentLanguage === "hr" ? "Lokalni razgovor" : "Local conversation"} · ${thread.messageCount || 0}`
          : (currentLanguage === "hr" ? "Lokalni sandbox" : "Local sandbox"));
      }
      const health = button.querySelector(":scope > i:last-child, :scope > svg:last-child");
      if (health) health.setAttribute("class", connection?.status === "connected" ? "health-ok" : "health-warn");
      button.title = connection
        ? `${connection.status} · ${connection.mode}`
        : (currentLanguage === "hr" ? "Asistent radi lokalno; vanjski račun nije povezan." : "The assistant works locally; no external account is connected.");
    });
  }

  async function loadAssistantMessages() {
    if (!activeAssistantThreadID) {
      assistantMessages = [];
      renderAssistant();
      return;
    }
    assistantMessages = await apiRequest(`/projects/${projectID}/assistant/threads/${activeAssistantThreadID}/messages`);
    const activeThread = assistantThreads.find((candidate) => candidate.id === activeAssistantThreadID);
    if (activeThread) activeThread.messageCount = assistantMessages.length;
    renderAssistant();
  }

  async function createAssistantThread(title = currentLanguage === "hr" ? "Razgovor s Millenom" : "Conversation with Millena", channel = assistantChannel) {
    const created = await apiRequest(`/projects/${projectID}/assistant/threads`, {
      method: "POST", body: JSON.stringify({ title, channel }),
    });
    assistantThreads.unshift(created);
    activeAssistantThreadID = created.id;
    assistantMessages = [];
    renderAssistant();
    return created;
  }

  async function loadAssistant() {
    [assistantStatus, assistantThreads] = await Promise.all([
      apiRequest(`/projects/${projectID}/assistant/status`),
      apiRequest(`/projects/${projectID}/assistant/threads`),
    ]);
    if (!activeAssistantThreadID || !assistantThreads.some((thread) => thread.id === activeAssistantThreadID)) {
      activeAssistantThreadID = assistantThreads[0]?.id || "";
    }
    assistantChannel = assistantThreads.find((thread) => thread.id === activeAssistantThreadID)?.channel || "app";
    if (!activeAssistantThreadID) await createAssistantThread();
    await loadAssistantMessages();
  }

  async function sendAssistantMessage() {
    const input = document.querySelector("#assistant-input");
    const button = document.querySelector("#assistant-send");
    const body = input.value.trim();
    if (body.length < 2 && !pendingAssistantAssets.length) return;
    if (!activeAssistantThreadID) await createAssistantThread();
    input.disabled = true;
    button.disabled = true;
    try {
      const result = await apiRequest(`/projects/${projectID}/assistant/threads/${activeAssistantThreadID}/messages`, {
        method: "POST", body: JSON.stringify({
          body: body || (currentLanguage === "hr" ? "Analiziraj priloženi kontekst." : "Review the attached context."),
          attachmentIds: pendingAssistantAssets.map((asset) => asset.id),
        }),
      });
      input.value = "";
      pendingAssistantAssets = [];
      renderPendingAssistantAssets();
      assistantMessages.push(result.userMessage, result.assistantMessage);
      const activeThread = assistantThreads.find((candidate) => candidate.id === activeAssistantThreadID);
      if (activeThread) {
        activeThread.messageCount = assistantMessages.length;
        activeThread.lastMessage = result.assistantMessage.body;
        activeThread.updatedAt = result.assistantMessage.createdAt;
      }
      renderAssistant();
      if (result.createdContentId || result.affectedRuleId) {
        await Promise.all([loadContent(), loadAutomations(), loadDashboard()]);
      }
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "AI asistent" : "AI assistant", error);
    } finally {
      input.disabled = false;
      button.disabled = false;
      input.focus();
    }
  }

  function renderPendingAssistantAssets() {
    const root = document.querySelector("#assistant-attachments");
    if (!root) return;
    root.replaceChildren();
    pendingAssistantAssets.forEach((asset) => root.append(renderAssetChip(asset, async (selected) => {
      try {
        await deleteProjectAsset(selected);
        pendingAssistantAssets = pendingAssistantAssets.filter((item) => item.id !== selected.id);
        renderPendingAssistantAssets();
      } catch (error) {
        showDomainError(currentLanguage === "hr" ? "Brisanje privitka" : "Deleting attachment", error);
      }
    })));
    root.hidden = pendingAssistantAssets.length === 0;
    refreshIcons();
  }

  async function uploadAssistantAttachments(input) {
    const files = [...(input.files || [])];
    input.value = "";
    if (!files.length) return;
    if (pendingAssistantAssets.length + files.length > 5) {
      openActionModal(currentLanguage === "hr" ? "Najviše pet privitaka po poruci" : "Up to five attachments per message", currentLanguage === "hr" ? "Uklonite neki privitak ili pošaljite novu poruku." : "Remove an attachment or send another message.", currentLanguage === "hr" ? "Previše privitaka" : "Too many attachments");
      return;
    }
    const attachButton = document.querySelector("[data-assistant-attach]");
    if (attachButton) attachButton.disabled = true;
    try {
      for (const file of files) {
        pendingAssistantAssets.push(await uploadProjectAsset(file, "assistant_attachment"));
        renderPendingAssistantAssets();
      }
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Upload privitka" : "Attachment upload", error);
    } finally {
      if (attachButton) attachButton.disabled = false;
    }
  }

  async function newAssistantThread() {
    const title = window.prompt(currentLanguage === "hr" ? "Naziv razgovora" : "Conversation title", currentLanguage === "hr" ? "Novi razgovor" : "New conversation");
    if (!title?.trim()) return;
    try {
      await createAssistantThread(title.trim());
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Novi razgovor" : "New conversation", error);
    }
  }

  async function switchAssistantChannel(channel, button) {
    assistantChannel = channel;
    document.querySelectorAll(".bot-channel").forEach((choice) => choice.classList.toggle("active", choice === button));
    const existing = assistantThreads.find((thread) => thread.channel === channel);
    try {
      if (existing) {
        activeAssistantThreadID = existing.id;
        await loadAssistantMessages();
      } else {
        await createAssistantThread(`${channel === "app" ? "App" : channel} · ${currentLanguage === "hr" ? "razgovor" : "conversation"}`, channel);
      }
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Promjena kanala asistenta" : "Switching assistant channel", error);
    }
  }

  function blogSEO(title, body) {
    const words = body.trim().split(/\s+/).filter(Boolean).length;
    let score = 20;
    const checks = [];
    if (title.length >= 25 && title.length <= 70) { score += 25; checks.push(currentLanguage === "hr" ? "Duljina naslova je dobra" : "Title length is good"); }
    else checks.push(currentLanguage === "hr" ? "Naslov neka ima 25–70 znakova" : "Keep the title between 25–70 characters");
    if (words >= 250) { score += 30; checks.push(currentLanguage === "hr" ? "Članak ima dovoljno sadržaja" : "The article has enough content"); }
    else checks.push(currentLanguage === "hr" ? `Dodajte još sadržaja (${words}/250 riječi)` : `Add more copy (${words}/250 words)`);
    const webSelected = Boolean(document.querySelector("#blog-publish-web")?.checked);
    const websiteConnection = connectionForProvider("website");
    if (webSelected && websiteConnection?.status === "connected") {
      score += 15;
      checks.push(currentLanguage === "hr" ? "Lokalni web kanal je povezan" : "Local website channel is connected");
    } else if (webSelected) {
      checks.push(currentLanguage === "hr" ? "Web je odabran, ali kanal nije povezan" : "Website is selected, but the channel is not connected");
    } else {
      checks.push(currentLanguage === "hr" ? "Odaberite web kanal ako je članak za objavu" : "Select the website channel if the article should be published");
    }
    if (document.querySelector("#blog-lead")?.innerText.trim().length >= 40) score += 10;
    return { score: Math.min(100, score), checks };
  }

  function renderBlogSEO() {
    const title = document.querySelector("#blog-title")?.innerText.trim() || "";
    const body = document.querySelector("#blog-body")?.innerText.trim() || "";
    const result = blogSEO(title, body);
    setText("[data-blog-seo-score]", `${result.score}/100`);
    const meter = document.querySelector(".seo-score > i > b");
    if (meter) meter.style.width = `${result.score}%`;
    const checks = document.querySelector("[data-blog-seo-checks]");
    if (checks) {
      checks.replaceChildren();
      result.checks.forEach((label) => {
        const item = document.createElement("li");
        item.textContent = label;
        checks.append(item);
      });
    }
  }

  function hydrateBlogAssets() {
    const item = contentItems.find((candidate) => candidate.id === blogContentID) || null;
    const bodyIDs = [...document.querySelectorAll("#blog-body img[data-asset-id]")].map((image) => image.dataset.assetId);
    const ids = [...new Set([...(item?.metadata?.assetIds || []), ...bodyIDs])];
    blogMediaAssets = ids.map((id) => projectAssets.find((asset) => asset.id === id)).filter(Boolean);
    const coverID = item?.metadata?.coverAssetId || blogMediaAssets[0]?.id || "";
    const coverAsset = blogMediaAssets.find((asset) => asset.id === coverID) || null;
    const cover = document.querySelector("#blog-cover");
    if (cover) {
      cover.hidden = !coverAsset;
      if (coverAsset) {
        cover.alt = coverAsset.filename;
        assetObjectURL(coverAsset).then((url) => { cover.src = url; }).catch((error) => console.error("Blog cover load failed", error));
      } else {
        cover.removeAttribute("src");
      }
    }
    document.querySelectorAll("#blog-body img[data-asset-id]").forEach((image) => {
      const asset = projectAssets.find((candidate) => candidate.id === image.dataset.assetId);
      if (!asset) {
        image.closest("figure")?.remove();
        return;
      }
      assetObjectURL(asset).then((url) => { image.src = url; }).catch((error) => console.error("Blog image load failed", error));
    });
    const root = document.querySelector("#blog-media-assets");
    if (root) {
      root.replaceChildren();
      blogMediaAssets.forEach((asset) => root.append(renderAssetChip(asset, removeBlogAsset)));
      root.hidden = blogMediaAssets.length === 0;
    }
    refreshIcons();
  }

  async function removeBlogAsset(asset) {
    blogMediaAssets = blogMediaAssets.filter((item) => item.id !== asset.id);
    document.querySelectorAll(`#blog-body img[data-asset-id="${asset.id}"]`).forEach((image) => (image.closest("figure") || image).remove());
    const usedElsewhere = contentItems.some((item) => item.id !== blogContentID && item.metadata?.assetIds?.includes(asset.id));
    try {
      if (!usedElsewhere) await deleteProjectAsset(asset);
      await saveBlog();
      hydrateBlogAssets();
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Brisanje slike" : "Deleting image", error);
    }
  }

  async function uploadBlogMedia(input) {
    const file = input.files?.[0];
    input.value = "";
    if (!file) return;
    try {
      const asset = await uploadProjectAsset(file, "content_media");
      blogMediaAssets.push(asset);
      const figure = document.createElement("figure");
      const image = document.createElement("img");
      image.dataset.assetId = asset.id;
      image.alt = asset.filename;
      const caption = document.createElement("figcaption");
      caption.textContent = asset.filename;
      figure.append(image, caption);
      document.querySelector("#blog-body")?.append(figure);
      hydrateBlogAssets();
      renderBlogSEO();
      await saveBlog();
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Upload slike" : "Image upload", error);
    }
  }

  function populateBlogAuthors(selectedValue = document.querySelector("#blog-author")?.value || "") {
    const select = document.querySelector("#blog-author");
    if (!select) return;
    const candidates = teamMembers
      .filter((member) => member.status === "active")
      .map((member) => ({ value: member.userId, label: member.displayName }));
    if (sessionUser && !candidates.some((item) => item.value === sessionUser.id)) {
      candidates.unshift({ value: sessionUser.id, label: sessionUser.displayName });
    }
    select.replaceChildren();
    candidates.forEach((candidate) => select.add(new Option(candidate.label, candidate.value)));
    if (selectedValue && !candidates.some((candidate) => candidate.value === selectedValue)) {
      select.add(new Option(selectedValue === "millena-insights" ? "Millena Insights" : selectedValue, selectedValue));
    }
    select.value = selectedValue && [...select.options].some((option) => option.value === selectedValue)
      ? selectedValue
      : candidates[0]?.value || "";
  }

  function renderSpecializedRecordSelectors() {
    const definitions = [
      ["#blog-record-select", "blog", blogContentID, currentLanguage === "hr" ? "Novi članak" : "New article"],
      ["#newsletter-record-select", "newsletter", newsletterContentID, currentLanguage === "hr" ? "Nova kampanja" : "New campaign"],
    ];
    definitions.forEach(([selector, kind, activeID, emptyLabel]) => {
      const select = document.querySelector(selector);
      if (!select) return;
      select.replaceChildren(new Option(emptyLabel, ""));
      contentItems.filter((item) => item.kind === kind).forEach((item) => {
        select.add(new Option(`${item.title} · ${contentStatusLabel(item.status)}`, item.id));
      });
      select.value = activeID && [...select.options].some((option) => option.value === activeID) ? activeID : "";
    });
    const newsletterTarget = document.querySelector("#blog-newsletter-target");
    if (newsletterTarget) {
      const activeBlog = contentItems.find((item) => item.id === blogContentID);
      const selected = activeBlog?.metadata?.newsletterTargetId || newsletterTarget.value || "";
      newsletterTarget.replaceChildren(new Option(currentLanguage === "hr" ? "Odaberite kampanju" : "Choose a campaign", ""));
      contentItems.filter((item) => item.kind === "newsletter").forEach((item) => {
        newsletterTarget.add(new Option(item.title, item.id));
      });
      newsletterTarget.value = [...newsletterTarget.options].some((option) => option.value === selected) ? selected : "";
      newsletterTarget.disabled = !document.querySelector("#blog-add-newsletter")?.checked;
    }
    const blogDelete = document.querySelector("#blog-delete");
    const newsletterDelete = document.querySelector("#newsletter-delete");
    if (blogDelete) blogDelete.hidden = !blogContentID;
    if (newsletterDelete) newsletterDelete.hidden = !newsletterContentID;
  }

  function confirmDiscardSpecialized(kind) {
    const dirty = kind === "blog" ? blogDirty : newsletterDirty;
    if (!dirty) return true;
    return window.confirm(currentLanguage === "hr" ? "Odbaciti nespremljene promjene?" : "Discard unsaved changes?");
  }

  function markBlogDirty() {
    blogDirty = true;
    const state = document.querySelector("#blog-save-state");
    if (!state) return;
    state.textContent = currentLanguage === "hr" ? "Nespremljene promjene" : "Unsaved changes";
    state.className = "dirty";
  }

  function newBlog() {
    blogContentID = "";
    blogMediaAssets = [];
    document.querySelector("#blog-title").textContent = currentLanguage === "hr" ? "Novi članak" : "New article";
    document.querySelector("#blog-lead").textContent = "";
    document.querySelector("#blog-body").replaceChildren();
    ensureSelectValue(document.querySelector("#blog-status"), "draft", currentLanguage === "hr" ? "U izradi" : "In progress");
    document.querySelector("#blog-publish-web").checked = false;
    document.querySelector("#blog-add-newsletter").checked = false;
    const newsletterTarget = document.querySelector("#blog-newsletter-target");
    if (newsletterTarget) {
      newsletterTarget.value = "";
      newsletterTarget.disabled = true;
    }
    ensureSelectValue(document.querySelector("#blog-category"), "insights", "Insights");
    populateBlogAuthors(sessionUser?.id || "");
    const authorLabel = document.querySelector("#blog-author-label");
    if (authorLabel) authorLabel.textContent = document.querySelector("#blog-author")?.selectedOptions?.[0]?.textContent || sessionUser?.displayName || "—";
    hydrateBlogAssets();
    renderSpecializedRecordSelectors();
    markBlogDirty();
    renderBlogSEO();
    document.querySelector("#blog-title")?.focus();
  }

  async function deleteSpecializedContent(kind) {
    const id = kind === "blog" ? blogContentID : newsletterContentID;
    if (!id) return;
    const item = contentItems.find((candidate) => candidate.id === id);
    const confirmed = window.confirm(currentLanguage === "hr" ? `Obrisati „${item?.title || "zapis"}”?` : `Delete “${item?.title || "entry"}”?`);
    if (!confirmed) return;
    try {
      await apiRequest(`/projects/${projectID}/content/items/${id}`, { method: "DELETE" });
      if (kind === "blog") newBlog();
      else newNewsletter();
      await Promise.all([loadContent(), loadDashboard()]);
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Brisanje sadržaja" : "Deleting content", error);
    }
  }

  function hydrateBlogEditor(item) {
    if (!item) return;
    blogContentID = item.id;
    document.querySelector("#blog-title").textContent = item.title;
    document.querySelector("#blog-lead").textContent = item.metadata?.lead || item.summary || "";
    document.querySelector("#blog-body").innerHTML = sanitizeEditableHTML(item.body || "");
    ensureSelectValue(document.querySelector("#blog-status"), item.status, item.status);
    populateBlogAuthors(item.metadata?.author || item.authorId || sessionUser?.id || "millena-insights");
    ensureSelectValue(document.querySelector("#blog-category"), item.metadata?.category || "insights", item.metadata?.category || "insights");
    document.querySelector("#blog-publish-web").checked = Boolean(item.metadata?.publishWeb);
    document.querySelector("#blog-add-newsletter").checked = Boolean(item.metadata?.addNewsletter);
    const newsletterTarget = document.querySelector("#blog-newsletter-target");
    if (newsletterTarget) {
      newsletterTarget.value = item.metadata?.newsletterTargetId || "";
      newsletterTarget.disabled = !item.metadata?.addNewsletter;
    }
    const category = document.querySelector(".article-category");
    if (category) category.textContent = (item.metadata?.category || "insights").replace("-", " ").toUpperCase();
    const authorLabel = document.querySelector("#blog-author-label");
    if (authorLabel) authorLabel.textContent = document.querySelector("#blog-author")?.selectedOptions?.[0]?.textContent || item.metadata?.author || "Millena Insights";
    hydrateBlogAssets();
    renderBlogSEO();
    const saveState = document.querySelector("#blog-save-state");
    if (saveState) {
      saveState.textContent = `${currentLanguage === "hr" ? "Spremljeno" : "Saved"} · rev ${item.revision} · ${formatDateTime(item.updatedAt)}`;
      saveState.className = "saved";
    }
    blogDirty = false;
    renderSpecializedRecordSelectors();
  }

  function hydrateNewsletterEditor(item) {
    if (!item) return;
    newsletterContentID = item.id;
    newsletterDraftIsNew = false;
    document.querySelector("#newsletter-title").textContent = item.title;
    document.querySelector("#newsletter-intro").textContent = item.metadata?.intro || item.summary || "";
    document.querySelector("#newsletter-subject").value = item.metadata?.subject || item.title;
    const listID = item.metadata?.listId || audienceLists.find((list) => list.isDefault)?.id || audienceLists[0]?.id || "";
    ensureSelectValue(document.querySelector("#newsletter-list"), listID, listID);
    document.querySelector("#newsletter-scheduled").value = item.scheduledFor ? localDateTimeValue(item.scheduledFor) : "";
    document.querySelectorAll("[data-newsletter-heading]").forEach((node) => { node.textContent = item.title; });
    document.querySelectorAll("[data-newsletter-status]").forEach((node) => {
      node.textContent = contentStatusLabel(item.status);
      node.className = `status-pill ${contentStatusClass(item.status)}`;
    });
    document.querySelectorAll("[data-newsletter-meta]").forEach((node) => {
      node.textContent = `${currentLanguage === "hr" ? "Revizija" : "Revision"} ${item.revision} · ${formatDateTime(item.updatedAt)}`;
    });
    newsletterBlockSelection = [...(item.metadata?.blocks || [])];
    renderNewsletterBlocks(newsletterBlockSelection);
    renderNewsletterEstimate();
    newsletterDirty = false;
    renderSpecializedRecordSelectors();
  }

  function markNewsletterDirty() {
    newsletterDirty = true;
    document.querySelectorAll("[data-newsletter-meta]").forEach((node) => {
      node.textContent = currentLanguage === "hr" ? "Nespremljene promjene" : "Unsaved changes";
    });
  }

  function hydrateContentEditors(force = false) {
    const blogItems = contentItems.filter((item) => item.kind === "blog");
    const newsletterItems = contentItems.filter((item) => item.kind === "newsletter");
    const blog = (blogContentID && blogItems.find((item) => item.id === blogContentID)) || blogItems[0];
    const newsletter = (newsletterContentID && newsletterItems.find((item) => item.id === newsletterContentID)) || newsletterItems[0];
    const blogSelectionMissing = Boolean(blogContentID) && !blogItems.some((item) => item.id === blogContentID);
    const newsletterSelectionMissing = Boolean(newsletterContentID) && !newsletterItems.some((item) => item.id === newsletterContentID);
    if (blog && (force || !blogContentID || blogSelectionMissing)) hydrateBlogEditor(blog);
    if (newsletter && !newsletterDraftIsNew && (force || !newsletterContentID || newsletterSelectionMissing)) hydrateNewsletterEditor(newsletter);
    if (!blog) renderBlogSEO();
    if (!newsletter && !newsletterDraftIsNew) renderNewsletterBlocks([]);
    renderSpecializedRecordSelectors();
  }

  function blogPayload(statusOverride = "") {
    const body = sanitizeEditableHTML(document.querySelector("#blog-body")?.innerHTML || "");
    const lead = document.querySelector("#blog-lead")?.innerText.trim() || "";
    const publishWeb = statusOverride === "published" || document.querySelector("#blog-publish-web")?.checked || false;
    const addNewsletter = document.querySelector("#blog-add-newsletter")?.checked || false;
    const existing = contentItems.find((item) => item.id === blogContentID);
    return {
      kind: "blog",
      status: statusOverride || document.querySelector("#blog-status")?.value || "draft",
      title: document.querySelector("#blog-title")?.innerText.trim() || "",
      summary: lead.slice(0, 500),
      body,
      channels: [publishWeb ? "website" : "", addNewsletter ? "newsletter" : ""].filter(Boolean),
      scheduledFor: null,
      source: "manual",
      metadata: {
        ...(existing?.metadata || {}),
        editor: "blog-editor", lead,
        author: document.querySelector("#blog-author")?.value || "millena-insights",
        category: document.querySelector("#blog-category")?.value || "insights",
        publishWeb, addNewsletter,
        newsletterTargetId: addNewsletter ? (document.querySelector("#blog-newsletter-target")?.value || null) : null,
        assetIds: blogMediaAssets.map((asset) => asset.id),
        coverAssetId: blogMediaAssets[0]?.id || null,
      },
    };
  }

  async function saveBlog(statusOverride = "") {
    const payload = blogPayload(statusOverride);
    if (payload.title.length < 2) {
      openActionModal(currentLanguage === "hr" ? "Dodajte naslov članka" : "Add an article title", currentLanguage === "hr" ? "Naslov mora imati najmanje dva znaka." : "The title must contain at least two characters.", currentLanguage === "hr" ? "Članak nije spremljen" : "Article not saved");
      return null;
    }
    if (payload.metadata.addNewsletter && !payload.metadata.newsletterTargetId) {
      openActionModal(
        currentLanguage === "hr" ? "Odaberite newsletter kampanju" : "Choose a newsletter campaign",
        currentLanguage === "hr" ? "Članak se može dodati tek u konkretnu spremljenu kampanju." : "The article can only be added to a specific saved campaign.",
        currentLanguage === "hr" ? "Kampanja nije odabrana" : "No campaign selected",
      );
      return null;
    }
    const button = document.querySelector(statusOverride === "published" ? "#blog-publish" : "#blog-save");
    if (button) button.disabled = true;
    try {
      const item = await apiRequest(blogContentID ? `/projects/${projectID}/content/items/${blogContentID}` : `/projects/${projectID}/content/items`, {
        method: blogContentID ? "PUT" : "POST", body: JSON.stringify(payload),
      });
      blogContentID = item.id;
      await Promise.all([loadContent(), loadDashboard()]);
      hydrateBlogEditor(contentItems.find((candidate) => candidate.id === item.id) || item);
      showToast("contentSaved");
      return item;
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Spremanje bloga" : "Saving blog", error);
      return null;
    } finally {
      if (button) button.disabled = false;
    }
  }

  function insertBlogBlock(kind) {
    if (!projectRoleAllows("owner", "lead", "editor")) return;
    const editor = document.querySelector("#blog-body");
    if (!editor) return;
    if (kind === "columns") {
      const section = document.createElement("section");
      section.dataset.blogBlock = "columns";
      [currentLanguage === "hr" ? "Prvi stupac" : "First column", currentLanguage === "hr" ? "Drugi stupac" : "Second column"].forEach((heading) => {
        const column = document.createElement("div");
        const title = document.createElement("strong");
        title.textContent = heading;
        const copy = document.createElement("p");
        copy.textContent = currentLanguage === "hr" ? "Dodajte sadržaj stupca…" : "Add column copy…";
        column.append(title, copy);
        section.append(column);
      });
      editor.append(section);
      markBlogDirty();
      renderBlogSEO();
      return;
    }
    if (kind === "cta") {
      const label = window.prompt(currentLanguage === "hr" ? "Tekst poziva na akciju" : "Call-to-action label", currentLanguage === "hr" ? "Saznajte više" : "Learn more");
      if (!label?.trim()) return;
      const rawURL = window.prompt("URL", projectProfile?.websiteUrl || "https://");
      if (!rawURL) return;
      let url;
      try {
        url = new URL(rawURL);
        if (!['http:', 'https:'].includes(url.protocol)) throw new Error("unsupported protocol");
      } catch {
        openActionModal(rawURL, currentLanguage === "hr" ? "Koristite punu http:// ili https:// adresu." : "Use a full http:// or https:// URL.", currentLanguage === "hr" ? "CTA nije dodan" : "CTA not added");
        return;
      }
      const paragraph = document.createElement("p");
      paragraph.dataset.blogBlock = "cta";
      const link = document.createElement("a");
      link.href = url.href;
      link.textContent = label.trim();
      paragraph.append(link);
      editor.append(paragraph);
      markBlogDirty();
      renderBlogSEO();
      return;
    }
    const blocks = {
      heading: ["h2", currentLanguage === "hr" ? "Novi naslov odjeljka" : "New section heading"],
      text: ["p", currentLanguage === "hr" ? "Dodajte tekst odjeljka…" : "Add section copy…"],
      image: ["p", currentLanguage === "hr" ? "[Mjesto za sliku — dodajte opis i izvor]" : "[Image placeholder — add alt text and source]"],
      quote: ["blockquote", currentLanguage === "hr" ? "Istaknuti citat…" : "Highlighted quote…"],
      list: ["ul", ""],
      spacer: ["p", " "],
    };
    const [tag, text] = blocks[kind] || blocks.text;
    const node = document.createElement(tag);
    if (tag === "ul") {
      [currentLanguage === "hr" ? "Prva stavka" : "First item", currentLanguage === "hr" ? "Druga stavka" : "Second item"].forEach((value) => {
        const item = document.createElement("li");
        item.textContent = value;
        node.append(item);
      });
    } else {
      node.textContent = text;
    }
    editor.append(node);
    editor.focus();
    markBlogDirty();
    renderBlogSEO();
  }

  function previewBlog() {
    const payload = blogPayload();
    const body = document.querySelector("#blog-body")?.innerText.trim() || "";
    const words = body.split(/\s+/).filter(Boolean).length;
    const meta = `${payload.summary || (currentLanguage === "hr" ? "Bez uvoda" : "No lead")} · ${words} ${currentLanguage === "hr" ? "riječi" : "words"} · SEO ${blogSEO(payload.title, body).score}/100`;
    openActionModal(meta, body || (currentLanguage === "hr" ? "Članak još nema sadržaj." : "The article has no content yet."), payload.title || (currentLanguage === "hr" ? "Pregled članka" : "Article preview"));
  }

  function newsletterCandidateBlocks() {
    return contentItems
      .filter((item) => item.kind !== "newsletter" && ["approved", "published", "scheduled"].includes(item.status))
      .sort((left, right) => Number(Boolean(right.metadata?.addNewsletter)) - Number(Boolean(left.metadata?.addNewsletter)) || new Date(right.updatedAt) - new Date(left.updatedAt))
      .map((item) => item.id);
  }

  function renderNewsletterBlocks(blockIDs = []) {
    const root = document.querySelector("[data-newsletter-blocks]");
    if (!root) return;
    root.replaceChildren();
    const items = blockIDs.map((id) => contentItems.find((item) => item.id === id)).filter(Boolean);
    newsletterBlockSelection = items.map((item) => item.id);
    if (!items.length) {
      const empty = document.createElement("div");
      empty.className = "newsletter-empty-state";
      const icon = document.createElement("i");
      icon.dataset.lucide = "layout-template";
      const copy = document.createElement("span");
      copy.textContent = currentLanguage === "hr" ? "Nema odabranih priča. Izbornik kampanje može dodati odobrene sadržaje." : "No stories selected. Use the campaign menu to add approved content.";
      empty.append(icon, copy);
      root.append(empty);
    } else {
      items.forEach((item) => {
        const article = document.createElement("article");
        article.className = "email-story";
        article.dataset.newsletterBlock = item.id;
        const title = document.createElement("strong");
        title.textContent = item.title;
        const summary = document.createElement("p");
        summary.textContent = item.summary || item.body.replace(/<[^>]+>/g, " ").slice(0, 180);
        const actions = document.createElement("span");
        actions.className = "email-story-actions";
        [["arrow-up", "up"], ["arrow-down", "down"], ["x", "remove"]].forEach(([iconName, action]) => {
          const button = document.createElement("button");
          button.type = "button";
          button.dataset.newsletterBlockAction = action;
          button.dataset.newsletterBlockId = item.id;
          const icon = document.createElement("i");
          icon.dataset.lucide = iconName;
          button.append(icon);
          actions.append(button);
        });
        article.append(title, summary, actions);
        root.append(article);
      });
    }
    if (hydrated) applyRoleUI();
    refreshIcons();
  }

  function openNewsletterBlockChooser() {
    if (!projectRoleAllows("owner", "lead", "editor")) return;
    const root = document.querySelector("#newsletter-block-chooser");
    if (!root) return;
    root.replaceChildren();
    const candidates = newsletterCandidateBlocks().map((id) => contentItems.find((item) => item.id === id)).filter(Boolean);
    candidates.forEach((item) => {
      const label = document.createElement("label");
      label.className = "newsletter-block-choice";
      const input = document.createElement("input");
      input.type = "checkbox";
      input.value = item.id;
      input.checked = newsletterBlockSelection.includes(item.id);
      const copy = document.createElement("span");
      const title = document.createElement("strong");
      title.textContent = item.title;
      const detail = document.createElement("small");
      detail.textContent = `${contentKindLabel(item.kind)} · ${contentStatusLabel(item.status)}${item.metadata?.addNewsletter ? ` · ${currentLanguage === "hr" ? "označeno u blogu" : "flagged in blog"}` : ""}`;
      copy.append(title, detail);
      const status = document.createElement("span");
      status.className = `status-pill ${contentStatusClass(item.status)}`;
      status.textContent = contentStatusLabel(item.status);
      label.append(input, copy, status);
      root.append(label);
    });
    if (!candidates.length) {
      root.textContent = currentLanguage === "hr" ? "Nema odobrenih, zakazanih ili objavljenih priča za odabir." : "There are no approved, scheduled, or published stories to choose from.";
    }
    if (hydrated) applyRoleUI();
    openDomainModal("newsletter-block-modal");
    refreshIcons();
  }

  async function saveNewsletterBlockSelection() {
    if (!projectRoleAllows("owner", "lead", "editor")) return;
    newsletterBlockSelection = [...document.querySelectorAll('#newsletter-block-chooser input[type="checkbox"]:checked')].map((input) => input.value);
    renderNewsletterBlocks(newsletterBlockSelection);
    markNewsletterDirty();
    closeDomainModal("newsletter-block-modal");
    if (document.querySelector("#newsletter-title")?.innerText.trim().length >= 2) await saveNewsletter(newsletterBlockSelection);
  }

  async function changeNewsletterBlock(id, action) {
    if (!projectRoleAllows("owner", "lead", "editor")) return;
    const index = newsletterBlockSelection.indexOf(id);
    if (index < 0) return;
    if (action === "remove") newsletterBlockSelection.splice(index, 1);
    if (action === "up" && index > 0) [newsletterBlockSelection[index - 1], newsletterBlockSelection[index]] = [newsletterBlockSelection[index], newsletterBlockSelection[index - 1]];
    if (action === "down" && index < newsletterBlockSelection.length - 1) [newsletterBlockSelection[index + 1], newsletterBlockSelection[index]] = [newsletterBlockSelection[index], newsletterBlockSelection[index + 1]];
    renderNewsletterBlocks(newsletterBlockSelection);
    markNewsletterDirty();
    if (newsletterContentID) await saveNewsletter(newsletterBlockSelection);
  }

  function renderNewsletterEstimate() {
    const listID = document.querySelector("#newsletter-list")?.value || "";
    const list = audienceLists.find((item) => item.id === listID);
    const scheduled = document.querySelector("#newsletter-scheduled")?.value;
    const strong = document.querySelector(".send-estimate strong");
    const small = document.querySelector(".send-estimate small");
    if (strong) strong.textContent = list
      ? `${list.activeCount} ${currentLanguage === "hr" ? "aktivnih primatelja s liste" : "active recipients in"} ${list.name}`
      : (currentLanguage === "hr" ? "Odaberite listu primatelja" : "Choose a recipient list");
    if (small) small.textContent = scheduled
      ? `${currentLanguage === "hr" ? "Sandbox dostava" : "Sandbox delivery"}: ${formatDateTime(new Date(scheduled).toISOString())}`
      : (currentLanguage === "hr" ? "Odaberite termin za zakazanu sandbox dostavu." : "Choose a time for the scheduled sandbox delivery.");
  }

  function newsletterPayload(blockIDs = null) {
    const title = document.querySelector("#newsletter-title")?.innerText.trim() || "";
    const intro = document.querySelector("#newsletter-intro")?.innerText.trim() || "";
    const existing = contentItems.find((item) => item.id === newsletterContentID);
    const blocks = blockIDs ?? existing?.metadata?.blocks ?? newsletterBlockSelection;
    const blockItems = blocks.map((id) => contentItems.find((item) => item.id === id)).filter(Boolean);
    const scheduledValue = document.querySelector("#newsletter-scheduled")?.value || "";
    const scheduledFor = scheduledValue ? new Date(scheduledValue).toISOString() : null;
    return {
      kind: "newsletter", status: existing?.status || "draft", title, summary: intro.slice(0, 500),
      body: [intro, ...blockItems.map((item) => `${item.title}\n${item.summary || item.body.replace(/<[^>]+>/g, " ")}`)].filter(Boolean).join("\n\n"),
      channels: ["newsletter"], scheduledFor, source: "manual",
      metadata: {
        ...(existing?.metadata || {}),
        editor: "newsletter-editor", intro,
        subject: document.querySelector("#newsletter-subject")?.value.trim() || title,
        listId: document.querySelector("#newsletter-list")?.value || null,
        blocks,
      },
    };
  }

  async function saveNewsletter(blockIDs = null) {
    const payload = newsletterPayload(blockIDs);
    if (payload.title.length < 2) {
      openActionModal(currentLanguage === "hr" ? "Dodajte naslov newslettera" : "Add a newsletter title", currentLanguage === "hr" ? "Naslov mora imati najmanje dva znaka." : "The title must contain at least two characters.", currentLanguage === "hr" ? "Kampanja nije spremljena" : "Campaign not saved");
      return null;
    }
    const button = document.querySelector("#newsletter-save");
    if (button) button.disabled = true;
    try {
      const item = await apiRequest(newsletterContentID ? `/projects/${projectID}/content/items/${newsletterContentID}` : `/projects/${projectID}/content/items`, {
        method: newsletterContentID ? "PUT" : "POST", body: JSON.stringify(payload),
      });
      newsletterContentID = item.id;
      newsletterDraftIsNew = false;
      await loadContent();
      hydrateNewsletterEditor(item);
      showToast("contentSaved");
      return item;
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Spremanje newslettera" : "Saving newsletter", error);
      return null;
    } finally {
      if (button) button.disabled = false;
    }
  }

  function renderNewsletterDeliveries() {
    const root = document.querySelector("#newsletter-deliveries");
    if (!root) return;
    root.replaceChildren();
    if (!newsletterDeliveries.length) {
      root.textContent = currentLanguage === "hr" ? "Još nema testnih ili zakazanih dostava." : "There are no test or scheduled deliveries yet.";
      return;
    }
    newsletterDeliveries.slice(0, 8).forEach((delivery) => {
      const item = document.createElement("div");
      const title = document.createElement("strong");
      title.textContent = delivery.subject;
      const detail = document.createElement("small");
      detail.textContent = `${delivery.testRecipient || `${delivery.recipientCount} ${currentLanguage === "hr" ? "primatelja" : "recipients"}`} · ${delivery.status} · ${formatDateTime(delivery.scheduledFor || delivery.sentAt || delivery.createdAt)}`;
      item.append(title, detail);
      root.append(item);
    });
  }

  async function loadNewsletterDeliveries() {
    newsletterDeliveries = await apiRequest(`/projects/${projectID}/newsletter/deliveries`);
    renderNewsletterDeliveries();
  }

  async function createNewsletterDelivery(testRecipient = null) {
    const content = await saveNewsletter();
    if (!content) return;
    const subject = document.querySelector("#newsletter-subject")?.value.trim() || content.title;
    const listID = document.querySelector("#newsletter-list")?.value;
    const scheduled = document.querySelector("#newsletter-scheduled")?.value;
    if (!testRecipient && (!listID || listID === "__new__" || !scheduled)) {
      openActionModal(currentLanguage === "hr" ? "Odaberite listu i budući termin" : "Choose a list and future time", currentLanguage === "hr" ? "Zakazana dostava treba stvarnu listu publike i vrijeme slanja." : "A scheduled delivery needs a real audience list and send time.", currentLanguage === "hr" ? "Dostava nije zakazana" : "Delivery not scheduled");
      return;
    }
    try {
      await apiRequest(`/projects/${projectID}/newsletter/deliveries`, {
        method: "POST",
        body: JSON.stringify({
          contentItemId: content.id, listId: testRecipient ? null : listID, mode: "sandbox", subject,
          testRecipient: testRecipient || null, scheduledFor: testRecipient ? null : new Date(scheduled).toISOString(),
        }),
      });
      await Promise.all([loadNewsletterDeliveries(), loadContent(), loadDashboard()]);
      const updated = contentItems.find((item) => item.id === content.id);
      if (updated) hydrateNewsletterEditor(updated);
      showToast(testRecipient ? "saved" : "scheduled");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Newsletter dostava" : "Newsletter delivery", error);
    }
  }

  function newNewsletter() {
    newsletterContentID = "";
    newsletterDraftIsNew = true;
    document.querySelector("#newsletter-title").textContent = currentLanguage === "hr" ? "Novi newsletter" : "New newsletter";
    document.querySelector("#newsletter-intro").textContent = "";
    document.querySelector("#newsletter-subject").value = "";
    document.querySelector("#newsletter-scheduled").value = "";
    const defaultListID = audienceLists.find((list) => list.isDefault)?.id || "";
    const newsletterList = document.querySelector("#newsletter-list");
    if (defaultListID) ensureSelectValue(newsletterList, defaultListID, defaultListID);
    else if (newsletterList) newsletterList.value = "";
    newsletterBlockSelection = [];
    newsletterDirty = true;
    renderNewsletterBlocks([]);
    document.querySelectorAll("[data-newsletter-status]").forEach((node) => { node.textContent = currentLanguage === "hr" ? "Nova skica" : "New draft"; });
    document.querySelectorAll("[data-newsletter-heading]").forEach((node) => { node.textContent = currentLanguage === "hr" ? "Novi newsletter" : "New newsletter"; });
    document.querySelectorAll("[data-newsletter-meta]").forEach((node) => { node.textContent = currentLanguage === "hr" ? "Nova nespremljena kampanja" : "New unsaved campaign"; });
    renderSpecializedRecordSelectors();
    renderNewsletterEstimate();
    document.querySelector("#newsletter-title")?.focus();
  }

  function connectionForProvider(provider) {
    return channelConnections.find((connection) => connection.provider === provider) || null;
  }

  function renderWebsiteIntegration() {
    const connection = connectionForProvider("website") || connectionForProvider("custom_api");
    const published = contentItems.filter((item) => item.kind === "blog" && item.status === "published" && item.metadata?.publishWeb);
    const pending = contentItems.filter((item) => item.kind === "blog" && ["approved", "scheduled"].includes(item.status) && item.metadata?.publishWeb);
    const websiteContacts = audienceStats?.website || 0;
    const offerPrice = document.querySelector(".website-plan.featured .plan-price");
    if (offerPrice) {
      const labels = offerPrice.querySelectorAll("span");
      if (labels[0]) labels[0].textContent = currentLanguage === "hr" ? "Cijena" : "Price";
      if (offerPrice.querySelector("strong")) offerPrice.querySelector("strong").textContent = currentLanguage === "hr" ? "Po ponudi" : "On request";
      if (labels[1]) labels[1].textContent = currentLanguage === "hr" ? "prema opsegu projekta" : "based on project scope";
    }
    const status = document.querySelector("#website-status");
    if (status) {
      status.textContent = connection ? (currentLanguage === "hr" ? "Konfigurirano" : "Configured") : (currentLanguage === "hr" ? "Nije konfigurirano" : "Not configured");
      status.className = `status-pill ${connection ? "auto" : "working"}`;
    }
    document.querySelectorAll("[data-website-name]").forEach((node) => { node.textContent = connection?.displayName || (currentLanguage === "hr" ? "Web integracija" : "Website integration"); });
    document.querySelectorAll("[data-website-preview-url]").forEach((node) => { node.textContent = connection?.endpointUrl || "preview"; });
    setText("#website-articles", published.length);
    setText("#website-subscribers", websiteContacts);
    setText("#website-pending", pending.length);
    renderEntitlementBranding();
    const previewHeading = document.querySelector(".browser-preview main > h3");
    if (previewHeading) {
      previewHeading.textContent = published.length
        ? (currentLanguage === "hr" ? "Objavljeno iz baze sadržaja projekta" : "Published from the project's content database")
        : (currentLanguage === "hr" ? "Označite blog za web i objavite ga kako bi se pojavio ovdje." : "Mark a blog for the website and publish it to show it here.");
    }
    const preview = document.querySelector("[data-website-preview-items]");
    if (preview) {
      preview.replaceChildren();
      published.slice(0, 3).forEach((item) => {
        const row = document.createElement("button");
        row.type = "button";
        row.dataset.websiteContent = item.id;
        row.textContent = item.title;
        preview.append(row);
      });
    }
    if (connection) {
      document.querySelector("#website-url").value = connection.endpointUrl || "";
      const platform = connection.metadata?.platform || "wordpress";
      document.querySelectorAll("#website-platform [data-platform]").forEach((button) => {
        const selected = button.dataset.platform === platform;
        button.classList.toggle("selected", selected);
        button.setAttribute("aria-pressed", String(selected));
      });
    }
  }

  function renderChannelConnections() {
    document.querySelectorAll("[data-channel-provider]").forEach((card) => {
      const connection = connectionForProvider(card.dataset.channelProvider);
      card.classList.toggle("connected", Boolean(connection));
      const status = card.querySelector("[data-channel-status]");
      const account = card.querySelector("[data-channel-account]");
      if (status) {
        status.textContent = connection ? `${currentLanguage === "hr" ? "Konfigurirano" : "Configured"} · ${connection.mode}` : (currentLanguage === "hr" ? "Nije konfigurirano" : "Not configured");
        status.className = `status-pill ${connection ? "auto" : "working"}`;
      }
      if (account) account.textContent = connection ? (connection.accountHandle || connection.displayName) : "—";
      const description = card.querySelector("[data-channel-description]");
      if (description && card.dataset.channelProvider === "website") {
        description.textContent = connection
          ? `${connection.mode} · ${connection.endpointUrl || (currentLanguage === "hr" ? "bez endpointa" : "no endpoint")}`
          : (currentLanguage === "hr" ? "Povežite WordPress, Strapi ili vlastiti API." : "Connect WordPress, Strapi, or a custom API.");
      }
      if (connection) card.dataset.connectionId = connection.id;
      else delete card.dataset.connectionId;
    });
    renderSetupChannelStatuses();
    renderAssistantChannelShortcuts();
    renderWebsiteIntegration();
    refreshIcons();
  }

  function renderSetupChannelStatuses() {
    document.querySelectorAll("[data-setup-channel]").forEach((row) => {
      const provider = row.dataset.setupChannel;
      const social = socialConnections.get(provider);
      const connection = connectionForProvider(provider);
      const account = row.querySelector("[data-channel-account]");
      const connected = Boolean(social || connection);
      row.classList.toggle("connected", connected);
      if (account) {
        account.textContent = social
          ? `${social.displayName} · ${social.accountHandle}`
          : connection
            ? (connection.accountHandle || connection.displayName)
            : (currentLanguage === "hr" ? "Nije povezano" : "Not connected");
      }
    });
  }

  async function loadChannelConnections() {
    channelConnections = await apiRequest(`/projects/${projectID}/channel-connections`);
    renderChannelConnections();
  }

  function openConnectionModal(provider, connection = connectionForProvider(provider)) {
    document.querySelector("#connection-id").value = connection?.id || "";
    ensureSelectValue(document.querySelector("#connection-provider"), connection?.provider || provider || "whatsapp", connection?.provider || provider || "whatsapp");
    const requestedMode = featureIncluded("api") ? (connection?.mode || "sandbox") : "sandbox";
    ensureSelectValue(document.querySelector("#connection-mode"), requestedMode, requestedMode);
    document.querySelector("#connection-display-name").value = connection?.displayName || provider || "";
    document.querySelector("#connection-handle").value = connection?.accountHandle || "";
    document.querySelector("#connection-endpoint").value = connection?.endpointUrl || "";
    document.querySelector("#connection-api-key").value = "";
    document.querySelector("#connection-delete").hidden = !connection;
    document.querySelector("#connection-test").hidden = !connection;
    openDomainModal("integration-modal");
    document.querySelector("#connection-display-name")?.focus();
  }

  function connectionPayload(existing = null) {
    const credential = document.querySelector("#connection-api-key").value;
    return {
      provider: document.querySelector("#connection-provider").value,
      mode: featureIncluded("api") ? document.querySelector("#connection-mode").value : "sandbox",
      displayName: document.querySelector("#connection-display-name").value.trim(),
      accountHandle: document.querySelector("#connection-handle").value.trim(),
      endpointUrl: document.querySelector("#connection-endpoint").value.trim(),
      credential,
      metadata: { ...(existing?.metadata || {}), editor: "integration-modal" },
    };
  }

  async function saveConnection(closeAfter = true) {
    const id = document.querySelector("#connection-id").value;
    const existing = channelConnections.find((connection) => connection.id === id) || null;
    const button = document.querySelector("#connection-save");
    button.disabled = true;
    try {
      const connection = await apiRequest(id ? `/projects/${projectID}/channel-connections/${id}` : `/projects/${projectID}/channel-connections`, {
        method: id ? "PUT" : "POST", body: JSON.stringify(connectionPayload(existing)),
      });
      document.querySelector("#connection-id").value = connection.id;
      if (closeAfter) closeDomainModal("integration-modal");
      await Promise.all([loadChannelConnections(), loadDashboard()]);
      showToast("saved");
      return connection;
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Spremanje veze" : "Saving connection", error);
      return null;
    } finally {
      button.disabled = false;
    }
  }

  async function deleteConnection() {
    const id = document.querySelector("#connection-id").value;
    if (!id) return;
    const connection = channelConnections.find((candidate) => candidate.id === id);
    const label = connection?.displayName || connection?.accountHandle || connection?.provider || document.querySelector("#connection-display-name")?.value.trim() || (currentLanguage === "hr" ? "vezu" : "connection");
    const confirmed = window.confirm(currentLanguage === "hr"
      ? `Ukloniti vezu „${label}”?`
      : `Remove connection “${label}”?`);
    if (!confirmed) return;
    try {
      await apiRequest(`/projects/${projectID}/channel-connections/${id}`, { method: "DELETE" });
      closeDomainModal("integration-modal");
      await Promise.all([loadChannelConnections(), loadDashboard()]);
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Uklanjanje veze" : "Removing connection", error);
    }
  }

  async function testConnection() {
    let id = document.querySelector("#connection-id").value;
    if (!id) {
      const saved = await saveConnection(false);
      id = saved?.id || "";
    }
    if (!id) return;
    const button = document.querySelector("#connection-test");
    button.disabled = true;
    try {
      const tested = await apiRequest(`/projects/${projectID}/channel-connections/${id}/test`, { method: "POST" });
      await Promise.all([loadChannelConnections(), loadDashboard()]);
      openActionModal(
        tested.displayName,
        `${currentLanguage === "hr" ? "Lokalna polja i format su valjani; vanjski provider nije kontaktiran" : "Local fields and format are valid; the external provider was not contacted"} · ${tested.mode} · ${formatDateTime(tested.lastCheckedAt)}`,
        currentLanguage === "hr" ? "Lokalna konfiguracija je provjerena" : "Local configuration validated",
      );
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Test veze" : "Connection test", error);
    } finally {
      button.disabled = false;
    }
  }

  async function testWebsiteConnection() {
    const url = document.querySelector("#website-url")?.value.trim() || "";
    const credential = document.querySelector("#website-api-key")?.value || "";
    const selectedPlatform = document.querySelector("#website-platform [data-platform].selected")?.dataset.platform || "wordpress";
    const existing = connectionForProvider("website");
    if (!url) {
      document.querySelector("#website-url")?.reportValidity();
      return;
    }
    const payload = {
      provider: "website", mode: featureIncluded("api") && (credential || existing?.credentialConfigured) ? "api" : "sandbox",
      displayName: `${selectedPlatform[0].toUpperCase()}${selectedPlatform.slice(1)} website`,
      accountHandle: selectedPlatform, endpointUrl: url, credential,
      metadata: { ...(existing?.metadata || {}), platform: selectedPlatform, editor: "website-screen" },
    };
    const button = document.querySelector("#website-test");
    button.disabled = true;
    try {
      const connection = await apiRequest(existing ? `/projects/${projectID}/channel-connections/${existing.id}` : `/projects/${projectID}/channel-connections`, {
        method: existing ? "PUT" : "POST", body: JSON.stringify(payload),
      });
      await apiRequest(`/projects/${projectID}/channel-connections/${connection.id}/test`, { method: "POST" });
      document.querySelector("#website-api-key").value = "";
      await Promise.all([loadChannelConnections(), loadDashboard()]);
      openActionModal(
        connection.displayName,
        `${currentLanguage === "hr" ? "Lokalna konfiguracija je spremljena; web provider nije kontaktiran" : "Local configuration was saved; the website provider was not contacted"} · ${connection.mode}`,
        currentLanguage === "hr" ? "Lokalna web konfiguracija je provjerena" : "Local website configuration validated",
      );
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Web integracija" : "Website integration", error);
    } finally {
      button.disabled = false;
    }
  }

  async function createServiceRequest(requestType, summary, metadata = {}) {
    return apiRequest(`/projects/${projectID}/service-requests`, {
      method: "POST", body: JSON.stringify({ requestType, summary, metadata }),
    });
  }

  function activeServiceRequest(requestType) {
    return serviceRequests.find((request) => request.requestType === requestType && ["open", "in_progress"].includes(request.status)) || null;
  }

  function renderServiceRequests() {
    const request = activeServiceRequest("website_proposal");
    const button = document.querySelector("#website-request");
    if (!button) return;
    button.classList.toggle("requested-action", Boolean(request));
    button.dataset.requestId = request?.id || "";
    const label = button.querySelector("span");
    if (label) {
      label.textContent = request
        ? `${currentLanguage === "hr" ? "Zahtjev spremljen" : "Request saved"} · ${request.status}`
        : (currentLanguage === "hr" ? "Zatraži prijedlog weba" : "Request website proposal");
    }
    button.title = request ? `${request.id} · ${formatDateTime(request.updatedAt)}` : "";
    const cancel = document.querySelector("#website-request-cancel");
    if (cancel) cancel.hidden = !request;
  }

  async function loadServiceRequests() {
    serviceRequests = await apiRequest(`/projects/${projectID}/service-requests`);
    renderServiceRequests();
  }

  async function cancelWebsiteProposal() {
    const request = activeServiceRequest("website_proposal");
    if (!request) return;
    if (!window.confirm(currentLanguage === "hr" ? "Otkazati aktivni zahtjev za prijedlog weba?" : "Cancel the active website proposal request?")) return;
    const button = document.querySelector("#website-request-cancel");
    button.disabled = true;
    try {
      const updated = await apiRequest(`/projects/${projectID}/service-requests/${request.id}`, {
        method: "PUT", body: JSON.stringify({ status: "cancelled" }),
      });
      serviceRequests = serviceRequests.map((item) => item.id === updated.id ? updated : item);
      renderServiceRequests();
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Otkazivanje zahtjeva" : "Cancelling request", error);
    } finally {
      button.disabled = false;
    }
  }

  async function requestWebsiteProposal() {
    const button = document.querySelector("#website-request");
    const existing = activeServiceRequest("website_proposal");
    if (existing) {
      openActionModal(existing.id, `${currentLanguage === "hr" ? "Status zahtjeva" : "Request status"}: ${existing.status} · ${formatDateTime(existing.updatedAt)}`, currentLanguage === "hr" ? "Zahtjev za web je već spremljen" : "Website request already saved");
      return;
    }
    button.disabled = true;
    try {
      const request = await createServiceRequest(
        "website_proposal",
        currentLanguage === "hr" ? "Klijent traži prijedlog Millena weba povezanog s aktivnim projektom." : "Client requests a Millena website proposal connected to the active project.",
        {
          projectName: projectProfile?.projectName,
          websiteUrl: projectProfile?.websiteUrl || "",
          priority: featureIncluded("prioritySupport") ? "priority" : "standard",
          entitlementPlan: entitlement?.planCode || "",
        },
      );
      serviceRequests.unshift(request);
      renderServiceRequests();
      openActionModal(request.id, `${currentLanguage === "hr" ? "Status zahtjeva" : "Request status"}: ${request.status}`, currentLanguage === "hr" ? "Zahtjev za web je spremljen" : "Website request saved");
      showToast("website");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Zahtjev za web" : "Website request", error);
    } finally {
      button.disabled = false;
    }
  }

  function renderTeam() {
    const list = document.querySelector("#team-list");
    if (!list) return;
    list.replaceChildren();
    teamMembers.forEach((member) => {
      const row = document.createElement("div");
      row.dataset.teamMember = member.userId;
      const avatar = document.createElement("span");
      avatar.className = "avatar small";
      avatar.textContent = member.displayName.split(/\s+/).slice(0, 2).map((part) => part[0]).join("").toUpperCase();
      const copy = document.createElement("span");
      const name = document.createElement("strong");
      name.textContent = member.displayName;
      const email = document.createElement("small");
      email.textContent = member.email;
      copy.append(name, email);
      const role = document.createElement("span");
      role.className = `status-pill ${member.status === "active" ? "auto" : "working"}`;
      role.textContent = `${member.role} · ${member.status}`;
      const edit = document.createElement("button");
      edit.type = "button";
      edit.dataset.teamEdit = member.userId;
      edit.setAttribute("aria-label", currentLanguage === "hr" ? "Uredi člana" : "Edit member");
      const icon = document.createElement("i");
      icon.dataset.lucide = "pencil";
      edit.append(icon);
      row.append(avatar, copy, role, edit);
      list.append(row);
    });
    if (!teamMembers.length) {
      const empty = document.createElement("p");
      empty.className = "content-empty";
      empty.textContent = currentLanguage === "hr" ? "Projekt još nema aktivnih članova." : "This project has no active members yet.";
      list.append(empty);
    }
    renderAssistantAccess();
    refreshIcons();
  }

  async function loadTeam() {
    teamMembers = await apiRequest(`/projects/${projectID}/team`);
    renderTeam();
    populateBlogAuthors(document.querySelector("#blog-author")?.value || contentItems.find((item) => item.id === blogContentID)?.metadata?.author || sessionUser?.id || "");
  }

  function openTeamModal(member = null) {
    document.querySelector("#team-user-id").value = member?.userId || "";
    document.querySelector("#team-name").value = member?.displayName || "";
    document.querySelector("#team-email").value = member?.email || "";
    ensureSelectValue(document.querySelector("#team-role"), member?.role || "contributor", member?.role || "contributor");
    ensureSelectValue(document.querySelector("#team-status"), member?.status || "active", member?.status || "active");
    document.querySelector("#team-temp-password").value = "";
    document.querySelector("#team-name").disabled = Boolean(member);
    document.querySelector("#team-email").disabled = Boolean(member);
    document.querySelector("#team-temp-password").closest("label").hidden = Boolean(member);
    document.querySelector("#team-status").closest("label").hidden = !member;
    document.querySelector("#team-delete").hidden = !member;
    openDomainModal("team-modal");
    document.querySelector(member ? "#team-role" : "#team-name")?.focus();
  }

  async function saveTeamMember() {
    const id = document.querySelector("#team-user-id").value;
    const button = document.querySelector("#team-save");
    button.disabled = true;
    try {
      if (id) {
        await apiRequest(`/projects/${projectID}/team/${id}`, {
          method: "PUT", body: JSON.stringify({ role: document.querySelector("#team-role").value, status: document.querySelector("#team-status").value }),
        });
      } else {
        await apiRequest(`/projects/${projectID}/team`, {
          method: "POST", body: JSON.stringify({
            displayName: document.querySelector("#team-name").value.trim(),
            email: document.querySelector("#team-email").value.trim(),
            role: document.querySelector("#team-role").value,
            tempPassword: document.querySelector("#team-temp-password").value,
          }),
        });
      }
      closeDomainModal("team-modal");
      await loadTeam();
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Spremanje člana" : "Saving team member", error);
    } finally {
      button.disabled = false;
    }
  }

  async function deleteTeamMember() {
    const id = document.querySelector("#team-user-id").value;
    if (!id) return;
    const member = teamMembers.find((candidate) => candidate.userId === id);
    const label = member?.displayName || member?.email || document.querySelector("#team-name")?.value.trim() || (currentLanguage === "hr" ? "člana" : "member");
    const confirmed = window.confirm(currentLanguage === "hr"
      ? `Ukloniti člana „${label}” iz projekta?`
      : `Remove member “${label}” from the project?`);
    if (!confirmed) return;
    try {
      await apiRequest(`/projects/${projectID}/team/${id}`, { method: "DELETE" });
      closeDomainModal("team-modal");
      await loadTeam();
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Uklanjanje člana" : "Removing team member", error);
    }
  }

  function planLimit(value, unit) {
    return value == null ? (currentLanguage === "hr" ? "Neograničeno" : "Unlimited") : `${new Intl.NumberFormat(currentLanguage === "hr" ? "hr-HR" : "en-GB").format(value)} ${unit}`;
  }

  function planStorageLimit(value) {
    if (value == null) return currentLanguage === "hr" ? "Neograničena pohrana" : "Unlimited storage";
    const gigabytes = value / (1024 * 1024 * 1024);
    return `${new Intl.NumberFormat(currentLanguage === "hr" ? "hr-HR" : "en-GB", { maximumFractionDigits: 1 }).format(gigabytes)} GB`;
  }

  function entitlementFeatureLabel(key, value) {
    const labels = {
      aiAgents: ["AI asistent i urednica", "AI assistant and editor"],
      analytics: ["Analitika", "Analytics"],
      api: ["API i webhook veze", "API and webhook connections"],
      auditLog: ["Dnevnik aktivnosti", "Audit log"],
      automations: ["Automatizacije", "Automations"],
      prioritySupport: ["Prioritetna podrška", "Priority support"],
      socialChannels: ["Društveni kanali", "Social channels"],
      whiteLabel: ["Vlastiti branding sadržaja", "Custom content branding"],
    };
    const label = labels[key]?.[currentLanguage === "hr" ? 0 : 1] || key.replace(/([A-Z])/g, " $1").replace(/^./, (letter) => letter.toUpperCase());
    if (key === "socialChannels" && value) {
      const amount = value === "all" ? (currentLanguage === "hr" ? "svi" : "all") : value;
      return `${label}: ${amount}`;
    }
    return label;
  }

  function featureIncluded(key) {
    return entitlement?.features?.[key] === true;
  }

  function projectRoleAllows(...roles) {
    return roles.includes(projectAccess?.role || "viewer");
  }

  function screenAllowed(screen) {
    if (!projectAccess || !entitlement) return true;
    if (screen === "automations") return featureIncluded("automations");
    if (screen === "companion") return featureIncluded("aiAgents");
    if (screen === "audience") return projectRoleAllows("owner", "lead", "editor");
    return true;
  }

  window.__millenaAuthorizeScreen = screenAllowed;

  function setControlLock(selectors, lockName, locked, copy) {
    selectors.forEach((selector) => document.querySelectorAll(selector).forEach((control) => {
      const key = `lock${lockName[0].toUpperCase()}${lockName.slice(1)}`;
      const hadLocks = Boolean(control.dataset.lockFeature || control.dataset.lockRole || control.dataset.lockChannelLimit || control.dataset.lockSchedule);
      if (locked && !hadLocks) {
        control.dataset.disabledBeforeLocks = String(Boolean(control.disabled));
        control.dataset.titleBeforeLocks = control.title || "";
      }
      if (locked) control.dataset[key] = copy;
      else delete control.dataset[key];
      const activeLocks = [control.dataset.lockFeature, control.dataset.lockRole, control.dataset.lockChannelLimit, control.dataset.lockSchedule].filter(Boolean);
      if (activeLocks.length) control.disabled = true;
      else if (hadLocks) {
        control.disabled = control.dataset.disabledBeforeLocks === "true";
        if (control.dataset.titleBeforeLocks) control.title = control.dataset.titleBeforeLocks;
        else control.removeAttribute("title");
        delete control.dataset.disabledBeforeLocks;
        delete control.dataset.titleBeforeLocks;
      }
      control.setAttribute("aria-disabled", String(activeLocks.length > 0 || Boolean(control.disabled)));
      if (activeLocks.length) control.title = activeLocks[0];
    }));
  }

  function applyRoleUI() {
    if (!projectAccess) return;
    const canPublish = projectRoleAllows("owner", "lead", "editor");
    const canManage = projectRoleAllows("owner", "lead");
    const isOwner = projectRoleAllows("owner");
    const roleCopy = currentLanguage === "hr"
      ? `Uloga „${projectAccess.role}” nema dopuštenje za ovu promjenu.`
      : `The “${projectAccess.role}” role cannot make this change.`;

    setControlLock([
      "[data-content-add]", "#content-save", "#content-ai-generate", "#content-ai-refine",
      "#content-item-title", "#content-item-kind", "#content-item-status", "#content-item-summary",
      "#content-item-channels", "#content-item-scheduled", "#content-item-body",
      "[data-calendar-add]", "#calendar-save", "[data-social-publish]", "#social-media-add", ".rewrite-button",
      "#calendar-item-title", "#calendar-item-summary", "#calendar-item-channel", "#calendar-item-status", "#calendar-item-scheduled",
      "#blog-new", "#blog-save", "#blog-publish", "#blog-media-input", "#blog-status", "#blog-author", "#blog-category",
      "#blog-publish-web", "#blog-add-newsletter", "#blog-newsletter-target", "[data-blog-block]",
      "#newsletter-new", "#newsletter-save", "#newsletter-schedule", "#newsletter-test", "#newsletter-block-save",
      "#newsletter-subject", "#newsletter-list", "#newsletter-scheduled", "[data-newsletter-menu]",
      "[data-newsletter-block-action]", "#newsletter-block-chooser input", "[data-editor-command]",
      "#strategy-save", "#strategy-file", "#strategy-persona-add", "#strategy-topic-add", "[data-strategy-field]",
      "[data-strategy-tone]", "[data-strategy-goals] button", "[data-strategy-topics] button", "[data-strategy-mode]",
      "[data-persona-select]", "[data-persona-edit]",
      "#assistant-new-thread", "#assistant-send", "#assistant-input", "[data-assistant-attach]",
      "#audience-import", ".add-contact-action", "#audience-list-new", "#contact-save",
    ], "role", !canPublish, roleCopy);

    setControlLock([
      "#calendar-delete", "#content-delete", "#blog-delete", "#newsletter-delete",
      "#automation-new", "#automation-save", "#automation-delete", "[data-automation-rule-key]",
      "#automation-name", "#automation-description", "#automation-kind", "#automation-channel", "#automation-review",
      "#automation-schedule", "#automation-enabled", "#automation-content-kind", "#automation-formats",
      "#automation-target-channels", "#automation-gap-days", "#automation-fact-check", "#automation-respect-forbidden", "#automation-cadence",
      "#automation-hour", "#automation-minute",
      "#social-automation-enabled", "[data-social-connect]", "[data-social-test]", "[data-social-disconnect]",
      "#social-connect-submit", "[data-channel-manage]", "#connection-save", "#connection-test", "#connection-delete",
      "#connection-provider", "#connection-mode", "#connection-display-name", "#connection-handle", "#connection-endpoint", "#connection-api-key",
      "#audience-list-edit", "#audience-list-delete", "#contact-delete",
      "#website-request", "#website-request-cancel", "#website-test", "#website-signup-save", "#website-url", "#website-api-key",
      "#website-platform [data-platform]", "#website-signup-headline", "#website-signup-copy", "#notification-button", "[data-persona-delete]",
      "#profile-project-name", "#profile-company-name", "#profile-website-url", "#profile-industry",
      "#profile-primary-language", "#profile-company-description", "#profile-social-posts",
      "#profile-newsletter-cadence", "#profile-timezone",
    ], "role", !canManage, roleCopy);

    setControlLock(["#team-add", "#team-save", "#team-delete", "#plan-manage", "#plan-apply", "#plan-custom-create"], "role", !isOwner, roleCopy);
    setControlLock(["#automation-run"], "role", !canPublish, roleCopy);
    setControlLock(['[data-screen-target="audience"]'], "role", !canPublish, roleCopy);

    ["#blog-title", "#blog-lead", "#blog-body", "#newsletter-title", "#newsletter-intro", ".post-editor"].forEach((selector) => {
      const editor = document.querySelector(selector);
      if (!editor) return;
      editor.setAttribute("contenteditable", String(canPublish));
      editor.setAttribute("aria-readonly", String(!canPublish));
      editor.classList.toggle("permission-readonly", !canPublish);
      if (!canPublish) editor.title = roleCopy;
    });

    if (!canManage) {
      const teamList = document.querySelector("#team-list");
      if (teamList && !teamMembers.length) {
        teamList.replaceChildren();
        const message = document.createElement("p");
        message.className = "content-empty";
        message.textContent = currentLanguage === "hr"
          ? "Popis tima dostupan je administratoru i voditelju projekta."
          : "The team list is available to project administrators and leads.";
        teamList.append(message);
      }
    }

    const rawSocialLimit = entitlement?.features?.socialChannels;
    const socialLimit = rawSocialLimit === "all" ? Number.POSITIVE_INFINITY : Number(rawSocialLimit);
    const limitReached = Number.isFinite(socialLimit) && socialConnections.size >= Math.max(0, socialLimit);
    const limitCopy = currentLanguage === "hr"
      ? `Dosegnut je limit povezanih društvenih računa (${Number.isFinite(socialLimit) ? socialLimit : "∞"}).`
      : `The connected social account limit has been reached (${Number.isFinite(socialLimit) ? socialLimit : "∞"}).`;
    document.querySelectorAll("[data-social-provider]").forEach((card) => {
      const provider = card.dataset.socialProvider;
      const alreadyConnected = socialConnections.has(provider);
      const connect = card.querySelector("[data-social-connect]");
      if (connect) setControlLock([`[data-social-provider="${provider}"] [data-social-connect]`], "channelLimit", !alreadyConnected && (limitReached || !Number.isFinite(socialLimit) && rawSocialLimit !== "all"), limitCopy);
    });
  }

  function renderEntitlementBranding() {
    const customBranding = featureIncluded("whiteLabel");
    const companyName = projectProfile?.companyName || projectAccess?.projectName || "";
    const newsletterBrand = document.querySelector(".email-brand strong");
    const newsletterLogo = document.querySelector(".email-brand .brand-symbol");
    if (newsletterBrand) newsletterBrand.textContent = customBranding && companyName ? companyName : "Millena AI";
    if (newsletterLogo) newsletterLogo.hidden = customBranding && Boolean(companyName);
    const previewBrand = document.querySelector(".browser-preview main > small");
    if (previewBrand) previewBrand.textContent = customBranding && companyName ? companyName : "Millena Insights";
  }

  function applyHonestIntegrationCopy() {
    const localize = (node, hr, en) => {
      if (!node) return;
      node.dataset.hr = hr;
      node.dataset.en = en;
      node.textContent = currentLanguage === "hr" ? hr : en;
    };
    document.querySelectorAll("[data-social-connect] span").forEach((node) => {
      localize(node, "Spoji sandbox", "Connect sandbox");
    });
    document.querySelector("#plan-feature-sso")?.closest("label")?.remove();
    const ensureOption = (selector, value, label) => {
      const select = document.querySelector(selector);
      if (select && ![...select.options].some((option) => option.value === value)) select.add(new Option(label, value));
    };
    [
      ["youtube", "YouTube"], ["x", "X"], ["reddit", "Reddit"], ["pinterest", "Pinterest"],
      ["threads", "Threads"], ["telegram", "Telegram"],
    ].forEach(([value, label]) => ensureOption("#automation-channel", value, label));
    [["reddit", "Reddit"], ["pinterest", "Pinterest"]].forEach(([value, label]) => ensureOption("#calendar-item-channel", value, label));
    const conditionalReview = document.querySelector('#automation-review option[value="conditional"]');
    const automaticReview = document.querySelector('#automation-review option[value="automatic"]');
    localize(conditionalReview, "Skica · pregled po potrebi", "Draft · review when needed");
    localize(automaticReview, "Automatski odobri", "Approve automatically");
    localize(
      document.querySelector(".signup-card > p"),
      "Potvrđeni kontakti spremaju se kroz lokalni API; vanjski web obrazac treba zaseban webhook adapter.",
      "Confirmed contacts are stored through the local API; an external website form needs a separate webhook adapter.",
    );
    localize(
      document.querySelector(".website-plan:not(.featured) > p"),
      "Spremite i provjerite lokalnu CMS/API konfiguraciju. Stvarna sinkronizacija zahtijeva provider adapter i upotrebljiv token.",
      "Save and validate the local CMS/API configuration. Real synchronization requires a provider adapter and a usable token.",
    );
    localize(
      document.querySelector("#social-connect-modal .modal-head small"),
      "Lokalna sandbox integracija",
      "Local sandbox integration",
    );
    localize(
      document.querySelector("#social-connect-title > span:first-child"),
      "Spoji sandbox račun za",
      "Connect a sandbox account for",
    );
  }

  function applyEntitlementUI() {
    const unavailableCopy = currentLanguage === "hr" ? "Nije uključeno u aktivni paket." : "Not included in the active plan.";
    setControlLock(["#content-ai-generate", "#content-ai-refine", ".rewrite-button", "#assistant-send", "#assistant-input", "#assistant-new-thread", "[data-assistant-attach]"], "feature", !featureIncluded("aiAgents"), unavailableCopy);
    const automationsAvailable = featureIncluded("automations");
    setControlLock(["#automation-new", "#automation-save", "#automation-run", "[data-automation-rule-key]"], "feature", !automationsAvailable, unavailableCopy);
    if (automationsAvailable) syncAutomationInputs();
    else document.querySelectorAll("input[data-automation-rule-key]").forEach((input) => { input.checked = false; });
    setControlLock(["#notification-button"], "feature", !featureIncluded("auditLog"), unavailableCopy);
    setControlLock(['[data-screen-target="automations"]'], "feature", !featureIncluded("automations"), unavailableCopy);
    setControlLock(['[data-screen-target="companion"]'], "feature", !featureIncluded("aiAgents"), unavailableCopy);
    const apiAvailable = featureIncluded("api");
    const connectionMode = document.querySelector("#connection-mode");
    document.querySelectorAll('#connection-mode option[value="api"], #connection-mode option[value="webhook"]').forEach((option) => { option.disabled = !apiAvailable; });
    if (!apiAvailable && connectionMode && connectionMode.value !== "sandbox") connectionMode.value = "sandbox";
    ["#connection-api-key", "#website-api-key"].forEach((selector) => {
      const input = document.querySelector(selector);
      if (!input) return;
      input.disabled = !apiAvailable;
      if (!apiAvailable) {
        input.value = "";
        input.title = unavailableCopy;
      } else if (input.title === unavailableCopy) input.removeAttribute("title");
    });
    const analyticsAvailable = featureIncluded("analytics") && dashboardData?.analyticsAvailable !== false;
    const analyticsMessage = currentLanguage === "hr" ? "Analitika nije uključena u aktivni paket." : "Analytics is not included in the active plan.";
    [
      '[data-screen="overview"] .stats-grid',
      "#dashboard-pipeline",
      '[data-screen="audience"] .audience-stats',
      ".website-metrics",
    ].forEach((selector) => document.querySelectorAll(selector).forEach((node) => {
      node.classList.toggle("feature-analytics-locked", !analyticsAvailable);
      if (!analyticsAvailable) node.dataset.featureMessage = analyticsMessage;
      else delete node.dataset.featureMessage;
      node.setAttribute("aria-disabled", String(!analyticsAvailable));
    }));
    renderEntitlementBranding();
    applyHonestIntegrationCopy();
    applyRoleUI();
  }

  function renderPlans() {
    const activePlan = plans.find((plan) => plan.code === entitlement?.planCode);
    setText("#plan-current", entitlement?.planName || activePlan?.name || entitlement?.planCode || "—");
    const description = document.querySelector("#plan-description");
    if (description) {
      const limits = `${planLimit(entitlement?.seatLimit, currentLanguage === "hr" ? "mjesta" : "seats")} · ${planLimit(entitlement?.monthlyPublicationLimit, currentLanguage === "hr" ? "objava/mj." : "posts/mo")} · ${planStorageLimit(entitlement?.storageLimitBytes)}`;
      description.textContent = [activePlan?.description, limits].filter(Boolean).join(" · ");
    }
    const features = document.querySelector("#plan-features");
    if (features) {
      features.replaceChildren();
      Object.entries(entitlement?.features || {}).filter(([key]) => key !== "sso").sort(([left], [right]) => left.localeCompare(right)).forEach(([key, value]) => {
        const enabled = value === true || value === "all" || (typeof value === "number" && value > 0);
        const item = document.createElement("span");
        item.classList.toggle("feature-disabled", !enabled);
        const icon = document.createElement("i");
        icon.dataset.lucide = enabled ? "check" : "x";
        item.append(icon, document.createTextNode(entitlementFeatureLabel(key, value)));
        features.append(item);
      });
    }
    const select = document.querySelector("#plan-select");
    if (select) {
      select.replaceChildren(new Option(currentLanguage === "hr" ? "Odaberite paket" : "Choose a plan", ""));
      plans.filter((plan) => plan.isActive).forEach((plan) => {
        select.add(new Option(`${plan.name} · ${(plan.priceCents / 100).toLocaleString(currentLanguage === "hr" ? "hr-HR" : "en-GB", { style: "currency", currency: plan.currency || "EUR" })}`, plan.code));
      });
      if (entitlement?.planCode) select.value = entitlement.planCode;
    }
    const featureInputs = {
      "#plan-feature-ai": "aiAgents",
      "#plan-feature-automations": "automations",
      "#plan-feature-audit": "auditLog",
      "#plan-feature-analytics": "analytics",
      "#plan-feature-api": "api",
      "#plan-feature-priority": "prioritySupport",
      "#plan-feature-white-label": "whiteLabel",
    };
    Object.entries(featureInputs).forEach(([selector, key]) => {
      const input = document.querySelector(selector);
      if (input) input.checked = Boolean(entitlement?.features?.[key]);
    });
    const socialAll = document.querySelector("#plan-feature-social-all");
    const socialCount = document.querySelector("#plan-feature-social-count");
    if (socialAll) socialAll.checked = entitlement?.features?.socialChannels === "all";
    if (socialCount) {
      const configured = Number(entitlement?.features?.socialChannels);
      socialCount.value = Number.isInteger(configured) && configured >= 0 ? String(configured) : (socialCount.value || "3");
      socialCount.disabled = Boolean(socialAll?.checked);
    }
    renderSessionIdentity();
    applyEntitlementUI();
    refreshIcons();
  }

  async function loadPlans() {
    [plans, entitlement] = await Promise.all([
      apiRequest(`/projects/${projectID}/plans`),
      apiRequest(`/projects/${projectID}/entitlement`),
    ]);
    if (projectAccess) projectAccess.entitlement = { ...projectAccess.entitlement, ...entitlement };
    renderPlans();
  }

  async function applyPlan(code = document.querySelector("#plan-select")?.value) {
    if (!code) return;
    const button = document.querySelector("#plan-apply");
    button.disabled = true;
    try {
      entitlement = await apiRequest(`/projects/${projectID}/entitlement`, {
        method: "PUT", body: JSON.stringify({ planCode: code }),
      });
      if (projectAccess) projectAccess.entitlement = { ...projectAccess.entitlement, ...entitlement };
      closeDomainModal("plan-modal");
      await loadPlans();
      if (featureIncluded("automations")) await loadAutomations();
      else {
        automationRules = [];
        renderAutomations();
      }
      if (featureIncluded("aiAgents")) await Promise.all([loadAIStatus(), loadAssistant()]);
      else {
        assistantStatus = null;
        assistantThreads = [];
        assistantMessages = [];
        renderAssistant();
      }
      await loadDashboard();
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Promjena paketa" : "Changing plan", error);
    } finally {
      button.disabled = false;
    }
  }

  function positiveIntegerOrNull(selector, fallback = null) {
    const value = Number.parseInt(document.querySelector(selector)?.value || "", 10);
    return Number.isInteger(value) && value > 0 ? value : fallback;
  }

  async function createCustomPlan() {
    const button = document.querySelector("#plan-custom-create");
    button.disabled = true;
    const selectedFeatures = {
      aiAgents: Boolean(document.querySelector("#plan-feature-ai")?.checked),
      automations: Boolean(document.querySelector("#plan-feature-automations")?.checked),
      auditLog: Boolean(document.querySelector("#plan-feature-audit")?.checked),
      analytics: Boolean(document.querySelector("#plan-feature-analytics")?.checked),
      api: Boolean(document.querySelector("#plan-feature-api")?.checked),
      prioritySupport: Boolean(document.querySelector("#plan-feature-priority")?.checked),
      whiteLabel: Boolean(document.querySelector("#plan-feature-white-label")?.checked),
      socialChannels: document.querySelector("#plan-feature-social-all")?.checked
        ? "all"
        : Math.max(0, Math.min(8, Number.parseInt(document.querySelector("#plan-feature-social-count")?.value || "0", 10) || 0)),
    };
    try {
      const plan = await apiRequest(`/projects/${projectID}/plans`, {
        method: "POST", body: JSON.stringify({
          code: document.querySelector("#plan-custom-code").value.trim(),
          name: document.querySelector("#plan-custom-name").value.trim(),
          description: document.querySelector("#plan-custom-description").value.trim(),
          priceCents: Math.max(0, Math.round(Number(document.querySelector("#plan-custom-price").value || 0) * 100)),
          currency: "EUR", billingInterval: "month",
          seatLimit: positiveIntegerOrNull("#plan-custom-seats", null),
          monthlyPublicationLimit: positiveIntegerOrNull("#plan-custom-publications", null),
          storageLimitBytes: (() => {
            const megabytes = positiveIntegerOrNull("#plan-custom-storage", null);
            return megabytes == null ? null : megabytes * 1024 * 1024;
          })(),
          features: selectedFeatures,
        }),
      });
      await loadPlans();
      document.querySelector("#plan-select").value = plan.code;
      showToast("saved");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Izrada paketa" : "Creating plan", error);
    } finally {
      button.disabled = false;
    }
  }

  function activeProjectPreference() {
    try { return window.localStorage.getItem("millena.activeProjectId") || ""; } catch { return ""; }
  }

  function setActiveProjectPreference(id) {
    try { window.localStorage.setItem("millena.activeProjectId", id); } catch { /* Local storage may be disabled. */ }
  }

  async function loadProjectChooser() {
    const projects = await apiRequest("/projects");
    const list = document.querySelector("#project-list");
    if (!list) return;
    list.replaceChildren();
    projects.forEach((project) => {
      const row = document.createElement("div");
      row.className = "project-choice-row";
      const button = document.createElement("button");
      button.type = "button";
      button.className = "project-choice";
      button.dataset.projectSelect = project.id;
      button.classList.toggle("active", project.id === projectID);
      const avatar = document.createElement("span");
      avatar.className = "project-avatar";
      avatar.textContent = project.name.split(/\s+/).slice(0, 3).map((part) => part[0]).join("").toUpperCase();
      const copy = document.createElement("span");
      const name = document.createElement("strong");
      name.textContent = project.name;
      const meta = document.createElement("small");
      const sessionAccess = window.__millenaProjectAccess?.find((item) => item.projectId === project.id);
      meta.textContent = `${project.slug} · ${sessionAccess?.role || project.status}`;
      copy.append(name, meta);
      const icon = document.createElement("i");
      icon.dataset.lucide = project.id === projectID ? "circle-check" : "chevron-right";
      button.append(avatar, copy, icon);
      row.append(button);
      if (projects.length > 1 && project.slug !== "millena-demo" && sessionAccess?.role === "owner") {
        const deleteButton = document.createElement("button");
        deleteButton.type = "button";
        deleteButton.className = "project-delete";
        deleteButton.dataset.projectDelete = project.id;
        deleteButton.title = currentLanguage === "hr" ? "Obriši projekt" : "Delete project";
        deleteButton.setAttribute("aria-label", deleteButton.title);
        const deleteIcon = document.createElement("i");
        deleteIcon.dataset.lucide = "trash-2";
        deleteButton.append(deleteIcon);
        row.append(deleteButton);
      }
      list.append(row);
    });
    refreshIcons();
  }

  async function openProjectChooser() {
    openDomainModal("project-modal");
    const newProjectForm = document.querySelector("#project-new-form");
    const newProjectToggle = document.querySelector("#project-new-toggle");
    const canCreate = projectRoleAllows("owner");
    if (newProjectToggle) newProjectToggle.hidden = !canCreate;
    if (newProjectForm) newProjectForm.hidden = true;
    if (newProjectToggle) newProjectToggle.setAttribute("aria-expanded", "false");
    try {
      await loadProjectChooser();
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Učitavanje projekata" : "Loading projects", error);
    }
  }

  async function createProject() {
    if (!projectRoleAllows("owner")) {
      showDomainError(currentLanguage === "hr" ? "Izrada projekta" : "Creating project", new Error(currentLanguage === "hr" ? "Samo administrator može dodati projekt." : "Only an administrator can add a project."));
      return;
    }
    const button = document.querySelector("#project-create");
    button.disabled = true;
    try {
      const project = await apiRequest("/projects", {
        method: "POST", body: JSON.stringify({
          name: document.querySelector("#project-new-name").value.trim(),
          slug: document.querySelector("#project-new-slug").value.trim(),
          defaultLocale: document.querySelector("#project-new-locale").value,
          adminProjectId: projectID,
        }),
      });
      const session = await apiRequest("/auth/me");
      window.__millenaProjectAccess = session.projects || [];
      setActiveProjectPreference(project.id);
      window.location.reload();
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Izrada projekta" : "Creating project", error);
      button.disabled = false;
    }
  }

  async function deleteProject(project) {
    const confirmed = window.confirm(currentLanguage === "hr"
      ? `Obrisati projekt “${project.name}” i sav njegov sadržaj? Ova se radnja ne može poništiti.`
      : `Delete “${project.name}” and all of its content? This action cannot be undone.`);
    if (!confirmed) return;
    try {
      await apiRequest(`/projects/${project.id}`, { method: "DELETE" });
      const session = await apiRequest("/auth/me");
      window.__millenaProjectAccess = session.projects || [];
      const nextProject = session.projects?.find((access) => access.projectId !== project.id);
      if (project.id === projectID && nextProject) setActiveProjectPreference(nextProject.projectId);
      await loadProjectChooser();
      if (project.id === projectID) window.location.reload();
      else showToast("deleted");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Brisanje projekta" : "Deleting project", error);
    }
  }

  async function runGlobalSearch() {
    const query = document.querySelector("#global-search")?.value.trim() || "";
    if (query.length < 2) return;
    try {
      const canSearchAudience = projectRoleAllows("owner", "lead", "editor");
      const [matchingContent, matchingContacts] = await Promise.all([
        apiRequest(`/projects/${projectID}/content?search=${encodeURIComponent(query)}`),
        canSearchAudience
          ? apiRequest(`/projects/${projectID}/audience/contacts?search=${encodeURIComponent(query)}`)
          : Promise.resolve({ items: [], stats: null }),
      ]);
      if (matchingContent.length) {
        await loadContent();
        contentSearch = query;
        const search = document.querySelector("#content-search");
        if (search) search.value = query;
        renderContent();
        navigateTo("content");
      } else if (matchingContacts.items?.length) {
        audienceContacts = matchingContacts.items;
        audienceStats = matchingContacts.stats;
        const search = document.querySelector("#audience-search");
        if (search) search.value = query;
        renderAudience();
        navigateTo("audience");
      } else {
        openActionModal(query, currentLanguage === "hr" ? "Nema sadržaja ni kontakata koji odgovaraju upitu." : "No content or contacts match this query.", currentLanguage === "hr" ? "Nema rezultata" : "No results");
      }
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Pretraživanje" : "Search", error);
    }
  }

  let notificationEvents = [];
  let notificationsExpanded = false;

  function notificationPresentation(event) {
    const action = String(event.action || "");
    const metadata = event.metadata || {};
    const labels = {
      "content.created": ["file-plus-2", currentLanguage === "hr" ? "Sadržaj je kreiran" : "Content created"],
      "content.updated": ["pencil", currentLanguage === "hr" ? "Sadržaj je ažuriran" : "Content updated"],
      "content.reviewed": ["badge-check", currentLanguage === "hr" ? "Sadržaj je odobren" : "Content approved"],
      "content.revision_requested": ["rotate-ccw", currentLanguage === "hr" ? "Zatražena je dorada sadržaja" : "Content revision requested"],
      "content.deleted": ["trash-2", currentLanguage === "hr" ? "Sadržaj je obrisan" : "Content deleted"],
    };
    const [icon, title] = labels[action] || [action.includes("failed") ? "triangle-alert" : "activity", metadata.label || action.replace(/[._]/g, " ")];
    const detail = metadata.comment || metadata.reviewer || metadata.label || event.entityType || (currentLanguage === "hr" ? "Aktivnost je spremljena u audit zapis." : "Activity was saved to the audit log.");
    return { icon, title, detail, tone: action.includes("revision") ? "review" : action.includes("failed") ? "failure" : "" };
  }

  function renderNotifications() {
    const list = document.querySelector("#notifications-list");
    if (!list) return;
    list.replaceChildren();
    const visible = notificationEvents.slice(0, notificationsExpanded ? notificationEvents.length : 6);
    if (!visible.length) {
      const empty = document.createElement("p");
      empty.className = "content-empty";
      empty.textContent = currentLanguage === "hr" ? "Još nema aktivnosti projekta." : "There is no project activity yet.";
      list.append(empty);
    }
    visible.forEach((event) => {
      const presentation = notificationPresentation(event);
      const row = document.createElement("button");
      row.type = "button";
      row.className = `notification-entry ${presentation.tone}`;
      const iconWrap = document.createElement("span");
      const icon = document.createElement("i");
      icon.dataset.lucide = presentation.icon;
      iconWrap.append(icon);
      const copy = document.createElement("div");
      const title = document.createElement("strong");
      title.textContent = presentation.title;
      const detail = document.createElement("small");
      detail.textContent = presentation.detail;
      const time = document.createElement("time");
      time.textContent = formatDateTime(event.createdAt);
      copy.append(title, detail, time);
      row.append(iconWrap, copy);
      list.append(row);
    });
    const more = document.querySelector("#notifications-view-more");
    if (more) {
      more.hidden = notificationEvents.length <= 6;
      more.querySelector("span").textContent = notificationsExpanded
        ? (currentLanguage === "hr" ? "Prikaži manje" : "Show less")
        : (currentLanguage === "hr" ? "Prikaži sve" : "View all");
      more.querySelector("i").dataset.lucide = notificationsExpanded ? "chevrons-up" : "chevrons-down";
    }
    refreshIcons();
  }

  async function openNotifications() {
    try {
      notificationEvents = await apiRequest(`/projects/${projectID}/actions`);
      notificationsExpanded = false;
      setText("#notifications-summary-title", `${notificationEvents.length} ${currentLanguage === "hr" ? "zapisa aktivnosti" : "activity records"}`);
      setText("#notifications-summary-copy", notificationEvents.length
        ? (currentLanguage === "hr" ? "Najnovije promjene, pregledi i radnje u ovom projektu." : "Latest changes, reviews, and actions in this project.")
        : (currentLanguage === "hr" ? "Nove radnje prikazat će se ovdje." : "New activity will appear here."));
      renderNotifications();
      openDomainModal("notifications-modal");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "Obavijesti i audit" : "Notifications and audit", error);
    }
  }

  function closeNotifications() { closeDomainModal("notifications-modal"); }

  function openActionModal(label, copy = "", title = "") {
    const modal = document.querySelector("#action-modal");
    if (!modal) return;
    const labelNode = modal.querySelector("#action-modal-label");
    if (labelNode) labelNode.textContent = label;
    const copyNode = modal.querySelector("#action-modal-copy");
    if (copyNode) {
      copyNode.textContent = copy;
      copyNode.dataset.hr = copy;
      copyNode.dataset.en = copy;
    }
    const titleNode = modal.querySelector("#action-modal-title");
    if (titleNode) {
      titleNode.textContent = title || (currentLanguage === "hr" ? "Informacije" : "Information");
      titleNode.dataset.hr = titleNode.textContent;
      titleNode.dataset.en = titleNode.textContent;
    }
    const icon = modal.querySelector(".action-modal-content > span [data-lucide]");
    if (icon) icon.setAttribute("data-lucide", /nije|not |failed|ograničenje|limitation/i.test(title) ? "circle-alert" : "info");
    modal.classList.add("open");
    modal.setAttribute("aria-hidden", "false");
    document.body.style.overflow = "hidden";
    refreshIcons();
  }

  function closeActionModal() {
    const modal = document.querySelector("#action-modal");
    if (!modal) return;
    modal.classList.remove("open");
    modal.setAttribute("aria-hidden", "true");
    document.body.style.overflow = "";
  }

  function handleEditorCommand(command) {
    if (!projectRoleAllows("owner", "lead", "editor")) return;
    const editor = document.querySelector(".post-editor");
    if (!editor) return;
    const selection = window.getSelection();
    const selectedRange = selection?.rangeCount ? selection.getRangeAt(0).cloneRange() : null;
    const rangeIsInsideEditor = Boolean(selectedRange && editor.contains(selectedRange.commonAncestorContainer));
    const selectedText = rangeIsInsideEditor ? selection.toString() : "";
    let replacement = "";
    if (command === "emoji") {
      replacement = "✨";
    } else if (command === "createLink") {
      const rawURL = window.prompt(
        currentLanguage === "hr" ? "Unesite punu URL adresu poveznice:" : "Enter the full link URL:",
        "https://",
      );
      if (!rawURL) return;
      let url;
      try {
        url = new URL(rawURL);
        if (!['http:', 'https:'].includes(url.protocol)) throw new Error("Unsupported URL protocol");
      } catch {
        openActionModal(
          currentLanguage === "hr" ? "Unesite URL koji počinje s https:// ili http://" : "Enter a URL that starts with https:// or http://",
          rawURL,
          currentLanguage === "hr" ? "Poveznica nije dodana" : "Link not added",
        );
        return;
      }
      replacement = selectedText ? `[${selectedText}](${url.href})` : url.href;
    } else if (command === "bold") {
      replacement = selectedText ? `**${selectedText}**` : "**tekst**";
    } else if (command === "insertUnorderedList") {
      replacement = selectedText
        ? selectedText.split("\n").map((line) => line.startsWith("- ") ? line : `- ${line}`).join("\n")
        : "- ";
    } else {
      return;
    }

    editor.focus();
    if (rangeIsInsideEditor && selection) {
      selection.removeAllRanges();
      selection.addRange(selectedRange);
    }
    if (!document.execCommand("insertText", false, replacement)) {
      const fallback = document.createTextNode(replacement);
      const range = selection?.rangeCount ? selection.getRangeAt(0) : document.createRange();
      if (!selection?.rangeCount) range.selectNodeContents(editor);
      range.deleteContents();
      range.insertNode(fallback);
      range.setStartAfter(fallback);
      range.collapse(true);
      selection?.removeAllRanges();
      selection?.addRange(range);
    }
    editor.dispatchEvent(new Event("input", { bubbles: true }));
  }

  async function refineSocialEditor() {
    const editor = document.querySelector(".post-editor");
    const button = document.querySelector(".rewrite-button");
    const body = editor?.innerText.trim() || "";
    if (!body) return;
    button.disabled = true;
    try {
      const result = await apiRequest(`/projects/${projectID}/content/ai`, {
        method: "POST",
        body: JSON.stringify({ operation: "refine", kind: "social", brief: "", title: "Društvena objava", body, language: socialVariantLocale() }),
      });
      editor.textContent = result.body;
      updateSocialQuality();
      scheduleSocialDraftSave();
      showToast("contentRefined");
    } catch (error) {
      showDomainError(currentLanguage === "hr" ? "AI dorada objave" : "AI social refinement", error);
    } finally {
      button.disabled = false;
    }
  }

  function renderSessionIdentity() {
    if (!sessionUser || !projectAccess) return;
    const initials = sessionUser.displayName
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((part) => part[0]?.toUpperCase())
      .join("") || "AD";
    const projectInitials = projectAccess.projectName
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 3)
      .map((part) => part[0]?.toUpperCase())
      .join("") || "PRJ";
    const activeEntitlement = entitlement || projectAccess.entitlement || {};
    const publicationLimit = activeEntitlement.monthlyPublicationLimit;
    const publishedThisMonth = dashboardData?.stats?.publishedThisMonth || 0;
    document.querySelectorAll(".project-switcher .project-avatar").forEach((node) => { node.textContent = projectInitials; });
    document.querySelectorAll(".project-switcher .project-copy strong").forEach((node) => { node.textContent = projectAccess.projectName; });
    const hasProjectChoice = (window.__millenaProjectAccess || []).length > 1;
    document.querySelectorAll(".project-switcher-chevron").forEach((node) => { node.hidden = !hasProjectChoice; });
    const planName = activeEntitlement.planName || activeEntitlement.planCode || "—";
    document.querySelectorAll(".project-switcher .project-progress").forEach((node) => {
      node.textContent = activeEntitlement.planCode === "unlimited" ? "∞" : String(planName).slice(0, 3).toUpperCase();
      node.title = planName;
    });
    document.querySelectorAll(".profile-button .avatar").forEach((node) => { node.textContent = initials; });
    document.querySelectorAll(".profile-button strong").forEach((node) => { node.textContent = sessionUser.displayName; });
    document.querySelectorAll(".profile-button small").forEach((node) => {
      node.dataset.hr = projectAccess.role === "owner" ? "Administrator klijenta" : projectAccess.role;
      node.dataset.en = projectAccess.role === "owner" ? "Client administrator" : projectAccess.role;
      node.textContent = node.dataset[currentLanguage];
    });
    const permissionsButton = document.querySelector("#permissions-manage");
    const teamAddButton = document.querySelector("#team-add");
    if (permissionsButton) {
      const isOwner = projectRoleAllows("owner");
      permissionsButton.hidden = !isOwner;
      if (isOwner && teamAddButton?.parentElement) teamAddButton.before(permissionsButton);
    }
    document.querySelectorAll(".usage-row strong").forEach((node) => {
      node.textContent = publicationLimit == null ? `${publishedThisMonth} / ∞` : `${publishedThisMonth} / ${publicationLimit}`;
      node.title = planName;
    });
    const usageTrack = document.querySelector(".usage-track span");
    if (usageTrack) usageTrack.style.width = `${publicationLimit == null ? 0 : Math.min(100, Math.round((publishedThisMonth / Math.max(1, publicationLimit)) * 100))}%`;
    document.querySelectorAll("[data-active-project-plan]").forEach((node) => { node.textContent = `${projectAccess.projectName} · ${planName}`; });
    document.querySelectorAll('[data-screen="settings"] .page-heading h1').forEach((node) => { node.textContent = projectAccess.projectName; });
  }

  function collectState() {
    return {
      schemaVersion: 2,
      language: currentLanguage,
      currentScreen,
      setupStep: currentSetupStep,
      calendarView,
      contentKind,
      contentStatus,
    };
  }

  function sanitizeEditableHTML(html) {
    const template = document.createElement("template");
    template.innerHTML = typeof html === "string" ? html : "";
    const allowedTags = new Set(["P", "BR", "STRONG", "EM", "B", "UL", "OL", "LI", "BLOCKQUOTE", "H1", "H2", "H3", "FIGURE", "FIGCAPTION", "IMG", "SECTION", "DIV", "A"]);

    [...template.content.querySelectorAll("script,style,iframe,object,embed,link,meta,svg,math")].forEach((node) => node.remove());
    [...template.content.querySelectorAll("*")].forEach((node) => {
      if (!allowedTags.has(node.tagName)) {
        node.replaceWith(document.createTextNode(node.textContent || ""));
        return;
      }
      if (node.tagName === "IMG") {
        const assetID = node.dataset.assetId || "";
        const alt = node.getAttribute("alt") || "";
        [...node.attributes].forEach((attribute) => node.removeAttribute(attribute.name));
        if (assetID) node.dataset.assetId = assetID;
        if (alt) node.setAttribute("alt", alt.slice(0, 300));
      } else if (node.tagName === "A") {
        const href = node.getAttribute("href") || "";
        let safeHref = "";
        try {
          const url = new URL(href);
          if (['http:', 'https:'].includes(url.protocol)) safeHref = url.href;
        } catch { /* invalid links become plain text */ }
        [...node.attributes].forEach((attribute) => node.removeAttribute(attribute.name));
        if (safeHref) {
          node.setAttribute("href", safeHref);
          node.setAttribute("rel", "noopener noreferrer");
        } else {
          node.replaceWith(document.createTextNode(node.textContent || ""));
        }
      } else {
        const blockType = node.dataset.blogBlock || "";
        [...node.attributes].forEach((attribute) => node.removeAttribute(attribute.name));
        if (["columns", "cta"].includes(blockType)) node.dataset.blogBlock = blockType;
      }
    });
    return template.innerHTML;
  }

  function restoreDerivedState() {
    const selectedStrategy = document.querySelector("[data-strategy-mode].selected");
    if (selectedStrategy) {
      const isUpload = selectedStrategy.dataset.strategyMode === "upload";
      const questions = document.querySelector(".strategy-questions");
      const upload = document.querySelector(".strategy-upload");
      if (questions) questions.hidden = isUpload;
      if (upload) upload.hidden = !isUpload;
    }

    refreshIcons();
  }

  function hydrateState(state) {
    if (!state || state.schemaVersion !== 2) return;

    if (messages[state.language]) applyLanguage(state.language);
    if (Number.isInteger(state.setupStep) && state.setupStep >= 1 && state.setupStep <= 5) {
      currentSetupStep = state.setupStep;
      updateSetupControls();
    }

    if (["week", "month"].includes(state.calendarView)) calendarView = state.calendarView;
    if (["all", "source", "social", "blog", "newsletter", "press_release", "case_study", "event"].includes(state.contentKind)) contentKind = state.contentKind;
    if (["", "draft", "in_review", "approved", "scheduled", "published", "failed"].includes(state.contentStatus)) contentStatus = state.contentStatus;
    document.querySelectorAll("[data-calendar-view]").forEach((button) => {
      const selected = button.dataset.calendarView === calendarView;
      button.classList.toggle("active", selected);
      button.setAttribute("aria-pressed", String(selected));
    });
    const status = document.querySelector("#content-status-filter");
    if (status) status.value = contentStatus;
    const hashScreen = window.location.hash.slice(1);
    const destination = validScreens.has(hashScreen)
      ? hashScreen
      : (validScreens.has(state.currentScreen) ? state.currentScreen : "overview");
    navigateTo(destination, { updateHash: false, scroll: false });
  }

  async function bootstrap() {
    setAPIStatus("connecting");
    try {
      const session = await apiRequest("/auth/me");
      sessionUser = session.user;
      window.__millenaProjectAccess = session.projects || [];
      const preferredProjectID = activeProjectPreference();
      projectAccess = session.projects?.find((access) => access.projectId === preferredProjectID) || session.projects?.[0];
      if (!projectAccess) throw new Error("No active project access");
      projectID = projectAccess.projectId;
      entitlement = { ...(projectAccess.entitlement || {}) };
      setActiveProjectPreference(projectID);
      renderSessionIdentity();
      const app = await apiRequest(`/projects/${projectID}/state`);
      revision = app.revision;
      hydrateState(app.state);
      const domainLoads = [
        ["social", loadSocialData], ["calendar", loadCalendar], ["strategy", loadStrategy], ["content", loadContent],
        ["content AI", loadAIStatus], ["profile", loadProfile], ["dashboard", loadDashboard],
        ["channel connections", loadChannelConnections], ["newsletter deliveries", loadNewsletterDeliveries],
        ["plans", loadPlans], ["assets", loadProjectAssets], ["project personas", loadProjectPersonas],
      ];
      if (featureIncluded("automations")) domainLoads.push(["automations", loadAutomations]);
      if (featureIncluded("aiAgents")) domainLoads.push(["assistant", loadAssistant]);
      if (projectRoleAllows("owner", "lead", "editor")) domainLoads.push(["audience", loadAudience]);
      if (projectRoleAllows("owner", "lead")) domainLoads.push(["team", loadTeam], ["service requests", loadServiceRequests]);
      const failedLoads = [];
      await Promise.all(domainLoads.map(([name, loader]) => loader().catch((error) => {
        console.error(`Millena ${name} bootstrap failed`, error);
        failedLoads.push(name);
        return null;
      })));
      hydrateContentEditors(true);
      hydrateSocialStudio(true);
      hydrateBlogAssets();
      renderDashboard();
      renderChannelConnections();
      renderAudience();
      renderProjectPersonas();
      renderServiceRequests();
      renderSessionIdentity();
      applyEntitlementUI();
      if (!screenAllowed(currentScreen)) navigateTo("overview");
      hydrated = true;
      setAPIStatus(failedLoads.length ? "error" : "connected");
      if (failedLoads.length) console.error(`Millena bootstrap incomplete: ${failedLoads.join(", ")}`);
    } catch (error) {
      console.error("Millena API bootstrap failed", error);
      setAPIStatus("error");
    }
  }

  function scheduleSave() {
    if (!hydrated || !projectID || !projectRoleAllows("owner", "lead", "editor", "contributor")) return;
    window.clearTimeout(saveTimer);
    saveTimer = window.setTimeout(saveState, 450);
  }

  async function saveState() {
    if (!hydrated || !projectID || !projectRoleAllows("owner", "lead", "editor", "contributor")) return;
    if (saveInFlight) {
      saveQueued = true;
      return;
    }

    saveInFlight = true;
    setAPIStatus("saving");
    try {
      const data = await apiRequest(`/projects/${projectID}/state`, {
        method: "PUT",
        body: JSON.stringify({ state: collectState() }),
      });
      revision = data.revision;
      setAPIStatus("connected");
    } catch (error) {
      console.error("Millena workspace autosave failed", error);
      setAPIStatus("error");
    } finally {
      saveInFlight = false;
      if (saveQueued) {
        saveQueued = false;
        saveState();
      }
    }
  }

  document.addEventListener("input", (event) => {
    if (event.target.matches?.('[contenteditable="true"]')) {
      delete event.target.dataset.hr;
      delete event.target.dataset.en;
      event.target.querySelectorAll?.("[data-hr][data-en]").forEach((node) => {
        delete node.dataset.hr;
        delete node.dataset.en;
      });
    }
  });
  document.addEventListener("click", (event) => {
    if (event.target.closest?.("[data-screen-target], [data-lang], [data-calendar-view], [data-content-kind], [data-setup-step] button, #setup-back, #setup-next")) {
      window.setTimeout(scheduleSave, 0);
    }
  });
  document.querySelectorAll("[data-lang]").forEach((button) => button.addEventListener("click", () => {
    window.setTimeout(() => {
      applyEntitlementUI();
      renderWebsiteIntegration();
      renderNewsletterEstimate();
      syncAutomationScheduleFields();
    }, 0);
  }));

  document.querySelectorAll("#profile-project-name, #profile-company-name, #profile-company-description, #profile-website-url, #profile-industry, #profile-primary-language, #profile-social-posts, #profile-newsletter-cadence, #profile-timezone").forEach((input) => {
    input.addEventListener("input", scheduleProfileSave);
    input.addEventListener("change", scheduleProfileSave);
  });
  document.querySelector("#setup-next")?.addEventListener("click", (event) => {
    if (currentSetupStep !== 5) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    saveProfile(true).then((saved) => {
      if (saved) {
        navigateTo("overview");
        scheduleSave();
      }
    });
  }, true);

  document.addEventListener("change", (event) => {
    if (event.target.matches?.("input[data-automation-rule-key]")) toggleAutomation(event.target);
  });
  document.querySelector("#automation-new")?.addEventListener("click", () => openAutomationModal());
  document.querySelector("#automation-rules")?.addEventListener("click", (event) => {
    const button = event.target.closest("[data-automation-edit]");
    if (!button) return;
    const rule = automationRules.find((candidate) => candidate.id === button.dataset.automationEdit);
    if (rule) openAutomationModal(rule);
  });
  document.querySelectorAll("[data-automation-close]").forEach((button) => button.addEventListener("click", () => closeDomainModal("automation-modal")));
  document.querySelector("#automation-modal")?.addEventListener("click", (event) => {
    if (event.target === event.currentTarget) closeDomainModal("automation-modal");
  });
  document.querySelector("#automation-save")?.addEventListener("click", saveAutomation);
  document.querySelector("#automation-delete")?.addEventListener("click", deleteAutomation);
  document.querySelector("#automation-run")?.addEventListener("click", runAutomation);
  document.querySelector("#automation-schedule")?.addEventListener("input", syncAutomationScheduleFields);

  document.querySelectorAll(".add-contact-action").forEach((button) => button.addEventListener("click", () => openContactEditor()));
  document.querySelector("#audience-contacts")?.addEventListener("click", (event) => {
    const button = event.target.closest("[data-contact-edit]");
    if (!button) return;
    const contact = audienceContacts.find((candidate) => candidate.id === button.dataset.contactEdit);
    if (contact) openContactEditor(contact);
  });
  document.querySelector("#contact-save")?.addEventListener("click", saveContact);
  document.querySelector("#contact-delete")?.addEventListener("click", deleteContact);
  document.querySelector("#audience-import")?.addEventListener("click", () => document.querySelector("#audience-csv-file")?.click());
  document.querySelector("#audience-csv-file")?.addEventListener("change", (event) => importAudienceCSV(event.currentTarget));
  document.querySelector("#audience-search")?.addEventListener("input", (event) => {
    const search = event.currentTarget.value.trim();
    window.clearTimeout(audienceSearchTimer);
    audienceSearchTimer = window.setTimeout(() => loadAudience(search).catch((error) => showDomainError(currentLanguage === "hr" ? "Pretraga publike" : "Audience search", error)), 350);
  });
  ["#contact-list", "#newsletter-list"].forEach((selector) => {
    document.querySelector(selector)?.addEventListener("change", (event) => {
      if (event.currentTarget.value === "__new__") createAudienceList(event.currentTarget);
      if (selector === "#newsletter-list") {
        renderNewsletterEstimate();
        markNewsletterDirty();
      }
    });
  });
  document.querySelector("#audience-list-filter")?.addEventListener("change", (event) => {
    activeAudienceListID = event.currentTarget.value;
    loadAudience().catch((error) => showDomainError(currentLanguage === "hr" ? "Učitavanje liste" : "Loading list", error));
  });
  document.querySelector("#audience-list-new")?.addEventListener("click", () => openAudienceListModal());
  document.querySelector("#audience-list-edit")?.addEventListener("click", () => {
    const list = audienceLists.find((candidate) => candidate.id === activeAudienceListID);
    if (list) openAudienceListModal(list);
    else openActionModal(currentLanguage === "hr" ? "Odaberite jednu listu" : "Choose a list", currentLanguage === "hr" ? "Za uređivanje najprije odaberite konkretnu listu umjesto prikaza svih kontakata." : "Choose a specific list instead of all contacts before editing.", currentLanguage === "hr" ? "Lista nije odabrana" : "No list selected");
  });
  document.querySelectorAll("[data-audience-list-close]").forEach((button) => button.addEventListener("click", () => closeDomainModal("audience-list-modal")));
  document.querySelector("#audience-list-modal")?.addEventListener("click", (event) => { if (event.target === event.currentTarget) closeDomainModal("audience-list-modal"); });
  document.querySelector("#audience-list-save")?.addEventListener("click", saveAudienceList);
  document.querySelector("#audience-list-delete")?.addEventListener("click", deleteAudienceList);

  document.querySelector("#assistant-send")?.addEventListener("click", sendAssistantMessage);
  document.querySelector("#assistant-input")?.addEventListener("keydown", (event) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      sendAssistantMessage();
    }
  });
  document.querySelector("#assistant-new-thread")?.addEventListener("click", newAssistantThread);
  document.querySelector(".bot-conversation-panel > header > button")?.addEventListener("click", () => {
    const thread = assistantThreads.find((candidate) => candidate.id === activeAssistantThreadID);
    openActionModal(
      thread?.title || (currentLanguage === "hr" ? "Nema aktivnog razgovora" : "No active conversation"),
      thread
        ? `${thread.channel} · ${thread.messageCount || assistantMessages.length} ${currentLanguage === "hr" ? "poruka" : "messages"} · ${formatDateTime(thread.updatedAt)}`
        : (currentLanguage === "hr" ? "Pokrenite novi razgovor s Millenom." : "Start a new conversation with Millena."),
      currentLanguage === "hr" ? "Detalji razgovora" : "Conversation details",
    );
  });
  document.querySelectorAll(".bot-channel").forEach((button) => button.addEventListener("click", () => {
    const label = button.querySelector("strong")?.textContent.toLocaleLowerCase("en") || "app";
    const channel = label.includes("whatsapp") ? "whatsapp" : label.includes("telegram") ? "telegram" : "app";
    switchAssistantChannel(channel, button);
  }));
  document.querySelector("[data-assistant-attach]")?.addEventListener("click", () => {
    document.querySelector("#assistant-attachment-input")?.click();
  });
  document.querySelector("#assistant-attachment-input")?.addEventListener("change", (event) => uploadAssistantAttachments(event.currentTarget));
  document.querySelector("#assistant-thread")?.addEventListener("click", (event) => {
    const button = event.target.closest("[data-assistant-entity]");
    if (!button) return;
    if (button.dataset.assistantAction.startsWith("content")) {
      const item = contentItems.find((candidate) => candidate.id === button.dataset.assistantEntity);
      if (item) openContentModal(item);
      else apiRequest(`/projects/${projectID}/content/items/${button.dataset.assistantEntity}`).then(openContentModal).catch((error) => showDomainError("Content", error));
    } else if (button.dataset.assistantAction.startsWith("automation")) {
      const rule = automationRules.find((candidate) => candidate.id === button.dataset.assistantEntity);
      if (rule) openAutomationModal(rule);
    }
  });

  document.querySelector("#blog-save")?.addEventListener("click", () => saveBlog());
  document.querySelector("#blog-publish")?.addEventListener("click", () => saveBlog("published"));
  document.querySelector("#blog-preview")?.addEventListener("click", previewBlog);
  document.querySelector("#blog-new")?.addEventListener("click", () => { if (confirmDiscardSpecialized("blog")) newBlog(); });
  document.querySelector("#blog-delete")?.addEventListener("click", () => deleteSpecializedContent("blog"));
  document.querySelector("#blog-record-select")?.addEventListener("change", (event) => {
    if (!confirmDiscardSpecialized("blog")) {
      event.currentTarget.value = blogContentID || "";
      return;
    }
    const item = contentItems.find((candidate) => candidate.id === event.currentTarget.value);
    if (item) hydrateBlogEditor(item);
    else newBlog();
  });
  document.querySelectorAll("[data-blog-block]").forEach((button) => button.addEventListener("click", () => {
    if (button.dataset.blogBlock === "image") document.querySelector("#blog-media-input")?.click();
    else insertBlogBlock(button.dataset.blogBlock);
  }));
  document.querySelector("#blog-media-input")?.addEventListener("change", (event) => uploadBlogMedia(event.currentTarget));
  document.querySelectorAll("[data-blog-inspector-tab]").forEach((button) => button.addEventListener("click", () => {
    const target = button.dataset.blogInspectorTab;
    document.querySelectorAll("[data-blog-inspector-tab]").forEach((choice) => choice.classList.toggle("active", choice === button));
    document.querySelectorAll("[data-blog-inspector-panel]").forEach((panel) => { panel.hidden = panel.dataset.blogInspectorPanel !== target; });
  }));
  document.querySelector("#blog-author")?.addEventListener("change", (event) => {
    const label = document.querySelector("#blog-author-label");
    if (label) label.textContent = event.currentTarget.selectedOptions?.[0]?.textContent || event.currentTarget.value;
    markBlogDirty();
  });
  document.querySelectorAll("#blog-title, #blog-lead, #blog-body, #blog-publish-web, #blog-add-newsletter, #blog-status, #blog-category").forEach((control) => {
    const update = () => { renderBlogSEO(); markBlogDirty(); };
    control.addEventListener("input", update);
    control.addEventListener("change", update);
  });
  document.querySelector("#blog-add-newsletter")?.addEventListener("change", (event) => {
    const target = document.querySelector("#blog-newsletter-target");
    if (target) target.disabled = !event.currentTarget.checked;
  });
  document.querySelector("#blog-newsletter-target")?.addEventListener("change", markBlogDirty);

  document.querySelector("#newsletter-save")?.addEventListener("click", () => saveNewsletter());
  document.querySelector("#newsletter-new")?.addEventListener("click", () => { if (confirmDiscardSpecialized("newsletter")) newNewsletter(); });
  document.querySelector("#newsletter-delete")?.addEventListener("click", () => deleteSpecializedContent("newsletter"));
  document.querySelector("#newsletter-record-select")?.addEventListener("change", (event) => {
    if (!confirmDiscardSpecialized("newsletter")) {
      event.currentTarget.value = newsletterContentID || "";
      return;
    }
    const item = contentItems.find((candidate) => candidate.id === event.currentTarget.value);
    if (item) hydrateNewsletterEditor(item);
    else newNewsletter();
  });
  document.querySelector("#newsletter-schedule")?.addEventListener("click", () => createNewsletterDelivery());
  document.querySelector("#newsletter-test")?.addEventListener("click", () => {
    const email = window.prompt(currentLanguage === "hr" ? "Email za probnu dostavu" : "Test delivery email", sessionUser?.email || "");
    if (email?.trim()) createNewsletterDelivery(email.trim());
  });
  document.querySelector("[data-newsletter-menu]")?.addEventListener("click", openNewsletterBlockChooser);
  document.querySelectorAll("[data-newsletter-block-close]").forEach((button) => button.addEventListener("click", () => closeDomainModal("newsletter-block-modal")));
  document.querySelector("#newsletter-block-modal")?.addEventListener("click", (event) => { if (event.target === event.currentTarget) closeDomainModal("newsletter-block-modal"); });
  document.querySelector("#newsletter-block-save")?.addEventListener("click", saveNewsletterBlockSelection);
  document.querySelector("[data-newsletter-blocks]")?.addEventListener("click", (event) => {
    const action = event.target.closest("[data-newsletter-block-action]");
    if (action) {
      event.stopPropagation();
      changeNewsletterBlock(action.dataset.newsletterBlockId, action.dataset.newsletterBlockAction);
      return;
    }
    const block = event.target.closest("[data-newsletter-block]");
    const item = block && contentItems.find((candidate) => candidate.id === block.dataset.newsletterBlock);
    if (item) openContentModal(item);
  });
  document.querySelectorAll("#newsletter-title, #newsletter-intro, #newsletter-subject").forEach((control) => {
    control.addEventListener("input", markNewsletterDirty);
    control.addEventListener("change", markNewsletterDirty);
  });
  document.querySelector("#newsletter-scheduled")?.addEventListener("change", () => {
    renderNewsletterEstimate();
    markNewsletterDirty();
  });

  document.querySelectorAll("[data-channel-manage]").forEach((button) => button.addEventListener("click", () => {
    const provider = button.closest("[data-channel-provider]")?.dataset.channelProvider;
    if (provider) openConnectionModal(provider);
  }));
  document.querySelectorAll("[data-integration-close]").forEach((button) => button.addEventListener("click", () => closeDomainModal("integration-modal")));
  document.querySelector("#integration-modal")?.addEventListener("click", (event) => {
    if (event.target === event.currentTarget) closeDomainModal("integration-modal");
  });
  document.querySelector("#connection-save")?.addEventListener("click", () => saveConnection());
  document.querySelector("#connection-delete")?.addEventListener("click", deleteConnection);
  document.querySelector("#connection-test")?.addEventListener("click", testConnection);
  document.querySelector("#website-test")?.addEventListener("click", testWebsiteConnection);
  document.querySelector("#website-request")?.addEventListener("click", requestWebsiteProposal);
  document.querySelector("#website-request-cancel")?.addEventListener("click", cancelWebsiteProposal);
  document.querySelector("#website-signup-save")?.addEventListener("click", () => saveProfile());
  document.querySelectorAll("#website-platform [data-platform]").forEach((button) => button.addEventListener("click", () => {
    if (button.getAttribute("aria-disabled") === "true") {
      openActionModal(currentLanguage === "hr" ? "API nije uključen" : "API is not included", button.title, currentLanguage === "hr" ? "Mogućnost paketa" : "Plan feature");
      return;
    }
    document.querySelectorAll("#website-platform [data-platform]").forEach((choice) => {
      const selected = choice === button;
      choice.classList.toggle("selected", selected);
      choice.setAttribute("aria-pressed", String(selected));
    });
  }));
  document.querySelector("#channel-guide")?.addEventListener("click", () => {
    openActionModal(
      currentLanguage === "hr" ? "Sandbox, API ili webhook" : "Sandbox, API, or webhook",
      currentLanguage === "hr"
        ? "Sandbox radi odmah lokalno. API/webhook obrazac trenutačno sprema i provjerava strukturu konfiguracije, ali ne kontaktira vanjski provider. Pohranjuje se samo otisak ključa; prava isporuka traži kriptirani token vault, OAuth i provider adapter."
        : "Sandbox works locally now. The API/webhook form currently stores and validates the configuration structure but does not contact an external provider. Only a key fingerprint is stored; real delivery requires an encrypted token vault, OAuth, and a provider adapter.",
      currentLanguage === "hr" ? "Upute za povezivanje" : "Connection guide",
    );
  });
  document.querySelectorAll("[data-password-toggle]").forEach((button) => button.addEventListener("click", () => {
    const input = document.querySelector(`#${button.dataset.passwordToggle}`);
    if (input) input.type = input.type === "password" ? "text" : "password";
  }));

  document.querySelector("#team-add")?.addEventListener("click", () => openTeamModal());
  document.querySelector("#permissions-manage")?.addEventListener("click", () => {
    if (!projectRoleAllows("owner")) return;
    openActionModal(
      currentLanguage === "hr" ? "To be done" : "To be done",
      currentLanguage === "hr" ? "Detaljno uređivanje pojedinačnih korisničkih prava bit će dostupno u sljedećoj fazi." : "Detailed editing of individual user permissions will be available in the next phase.",
      currentLanguage === "hr" ? "Upravljanje korisničkim pravima" : "User permission management",
    );
  });
  document.querySelector("#team-list")?.addEventListener("click", (event) => {
    const button = event.target.closest("[data-team-edit]");
    const member = button && teamMembers.find((candidate) => candidate.userId === button.dataset.teamEdit);
    if (member) openTeamModal(member);
  });
  document.querySelectorAll("[data-team-close]").forEach((button) => button.addEventListener("click", () => closeDomainModal("team-modal")));
  document.querySelector("#team-modal")?.addEventListener("click", (event) => { if (event.target === event.currentTarget) closeDomainModal("team-modal"); });
  document.querySelector("#team-save")?.addEventListener("click", saveTeamMember);
  document.querySelector("#team-delete")?.addEventListener("click", deleteTeamMember);

  document.querySelector("#plan-manage")?.addEventListener("click", () => openDomainModal("plan-modal"));
  document.querySelectorAll("[data-plan-close]").forEach((button) => button.addEventListener("click", () => closeDomainModal("plan-modal")));
  document.querySelector("#plan-modal")?.addEventListener("click", (event) => { if (event.target === event.currentTarget) closeDomainModal("plan-modal"); });
  document.querySelector("#plan-apply")?.addEventListener("click", () => applyPlan());
  document.querySelector("#plan-custom-create")?.addEventListener("click", createCustomPlan);
  document.querySelector("#plan-feature-social-all")?.addEventListener("change", (event) => {
    const count = document.querySelector("#plan-feature-social-count");
    if (count) count.disabled = event.currentTarget.checked;
  });

  document.querySelector("#project-switcher")?.addEventListener("click", openProjectChooser);
  document.querySelector("#project-new-toggle")?.addEventListener("click", (event) => {
    const form = document.querySelector("#project-new-form");
    if (!form) return;
    form.hidden = !form.hidden;
    event.currentTarget.setAttribute("aria-expanded", String(!form.hidden));
    if (!form.hidden) document.querySelector("#project-new-name")?.focus();
    refreshIcons();
  });
  document.querySelectorAll("[data-project-close]").forEach((button) => button.addEventListener("click", () => closeDomainModal("project-modal")));
  document.querySelector("#project-modal")?.addEventListener("click", (event) => { if (event.target === event.currentTarget) closeDomainModal("project-modal"); });
  document.querySelector("#project-list")?.addEventListener("click", (event) => {
    const deleteButton = event.target.closest("[data-project-delete]");
    if (deleteButton) {
      const project = (window.__millenaProjectAccess || []).find((candidate) => candidate.projectId === deleteButton.dataset.projectDelete);
      const listProject = project && { id: project.projectId, name: project.projectName };
      if (listProject) deleteProject(listProject);
      return;
    }
    const button = event.target.closest("[data-project-select]");
    if (!button || button.dataset.projectSelect === projectID) return;
    setActiveProjectPreference(button.dataset.projectSelect);
    window.location.reload();
  });
  document.querySelector("#project-create")?.addEventListener("click", createProject);
  document.querySelector("#project-new-name")?.addEventListener("input", (event) => {
    const slug = document.querySelector("#project-new-slug");
    if (!slug || slug.dataset.edited === "true") return;
    slug.value = event.currentTarget.value.toLocaleLowerCase("en").normalize("NFD").replace(/[\u0300-\u036f]/g, "").replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 70);
  });
  document.querySelector("#project-new-slug")?.addEventListener("input", (event) => { event.currentTarget.dataset.edited = "true"; });

  document.querySelector("#global-search")?.addEventListener("keydown", (event) => {
    if (event.key === "Enter") {
      event.preventDefault();
      runGlobalSearch();
    }
  });
  document.querySelector("#notification-button")?.addEventListener("click", openNotifications);
  document.querySelectorAll("[data-notifications-close]").forEach((button) => button.addEventListener("click", closeNotifications));
  document.querySelector("#notifications-modal")?.addEventListener("click", (event) => { if (event.target === event.currentTarget) closeNotifications(); });
  document.querySelector("#notifications-view-more")?.addEventListener("click", () => { notificationsExpanded = !notificationsExpanded; renderNotifications(); });
  document.querySelector("#dashboard-content-list")?.addEventListener("click", (event) => {
    const button = event.target.closest("[data-dashboard-content-id]");
    const item = button && contentItems.find((candidate) => candidate.id === button.dataset.dashboardContentId);
    if (item) openContentModal(item);
  });
  document.querySelector("#dashboard-today")?.addEventListener("click", (event) => {
    const button = event.target.closest("[data-dashboard-calendar-id]");
    if (!button) return;
    apiRequest(`/projects/${projectID}/calendar/items/${button.dataset.dashboardCalendarId}`).then(openCalendarModal).catch((error) => showDomainError("Calendar", error));
  });
  document.querySelector("#dashboard-channel-health")?.addEventListener("click", (event) => {
    const button = event.target.closest("[data-dashboard-channel]");
    if (button) openConnectionModal(button.dataset.dashboardChannel);
  });
  document.querySelector("[data-website-preview-items]")?.addEventListener("click", (event) => {
    const button = event.target.closest("[data-website-content]");
    const item = button && contentItems.find((candidate) => candidate.id === button.dataset.websiteContent);
    if (item) openContentModal(item);
  });

  document.querySelector(".rewrite-button")?.addEventListener("click", refineSocialEditor);
  document.querySelector("#social-preview")?.addEventListener("click", () => {
    const body = document.querySelector(".post-editor")?.innerText.trim() || "";
    const channel = document.querySelector("[data-social-channel].active")?.dataset.socialChannel || "—";
    openActionModal(
      `${channel} · ${body.length}/3000`,
      body || (currentLanguage === "hr" ? "Objava je prazna." : "The post is empty."),
      currentLanguage === "hr" ? "Pregled društvene objave" : "Social post preview",
    );
  });
  document.querySelector(".post-editor")?.addEventListener("input", () => {
    updateSocialQuality();
    scheduleSocialDraftSave();
  });
  document.querySelector("#social-source-open")?.addEventListener("click", () => {
    const item = contentItems.find((candidate) => candidate.id === activeSocialSourceID);
    if (item) openContentModal(item);
    else navigateTo("content");
  });
  document.querySelector("#social-content-select")?.addEventListener("change", async (event) => {
    const select = event.currentTarget;
    const next = select.value;
    const previous = socialNewRecordSelected ? "__new__" : activeSocialContentID;
    select.disabled = true;
    try {
      if (!await flushSocialDraftSave()) {
        select.value = previous || "__new__";
        showToast("socialError");
        return;
      }
      socialNewRecordSelected = next === "__new__";
      activeSocialContentID = socialNewRecordSelected ? "" : next;
      socialVariants = [];
      socialMediaAssets = [];
      socialDraftDirty = false;
      const editor = document.querySelector(".post-editor");
      if (editor) {
        editor.textContent = "";
        delete editor.dataset.contentId;
        delete editor.dataset.variantKey;
        delete editor.dataset.channel;
      }
      await hydrateSocialStudio(true);
    } finally {
      select.disabled = false;
    }
  });
  document.querySelectorAll("[data-social-channel]").forEach((button) => button.addEventListener("click", async () => {
    const nextChannel = button.dataset.socialChannel;
    const previousChannel = activeSocialChannel;
    if (!nextChannel || nextChannel === previousChannel) return;
    const channelButtons = [...document.querySelectorAll("[data-social-channel]")];
    channelButtons.forEach((choice) => { choice.disabled = true; });
    try {
      if (!await flushSocialDraftSave()) {
        channelButtons.forEach((choice) => choice.classList.toggle("active", choice.dataset.socialChannel === previousChannel));
        showToast("socialError");
        return;
      }
      activeSocialChannel = nextChannel;
      socialDraftDirty = false;
      await hydrateSocialStudio(true);
    } finally {
      channelButtons.forEach((choice) => { choice.disabled = false; });
    }
  }));
  document.querySelector("#social-media-add")?.addEventListener("click", () => document.querySelector("#social-media-input")?.click());
  document.querySelector("#social-media-input")?.addEventListener("change", (event) => uploadSocialMedia(event.currentTarget));

  document.querySelectorAll("[data-social-connect]").forEach((button) => {
    button.addEventListener("click", () => openSocialModal(button.closest("[data-social-provider]").dataset.socialProvider));
  });
  document.querySelectorAll("[data-social-test]").forEach((button) => {
    button.addEventListener("click", () => testSocialConnection(button.closest("[data-social-provider]").dataset.socialProvider));
  });
  document.querySelectorAll("[data-social-disconnect]").forEach((button) => {
    button.addEventListener("click", () => disconnectSocialAccount(button.closest("[data-social-provider]").dataset.socialProvider));
  });
  document.querySelectorAll("[data-social-modal-close]").forEach((button) => button.addEventListener("click", closeSocialModal));
  document.querySelector("#social-connect-modal")?.addEventListener("click", (event) => {
    if (event.target === event.currentTarget) closeSocialModal();
  });
  document.querySelector("#social-connect-submit")?.addEventListener("click", connectSocialAccount);
  document.querySelectorAll("[data-social-publish]").forEach((button) => button.addEventListener("click", publishSocialPost));
  document.querySelector("[data-social-history-refresh]")?.addEventListener("click", () => loadSocialPosts().catch((error) => {
    console.error("Millena social history refresh failed", error);
    showToast("socialError");
  }));
  document.querySelector("[data-content-add]")?.addEventListener("click", () => openContentModal());
  document.querySelectorAll("[data-content-kind]").forEach((button) => button.addEventListener("click", () => {
    contentKind = button.dataset.contentKind;
    renderContent();
  }));
  document.querySelector("#content-status-filter")?.addEventListener("change", (event) => {
    contentStatus = event.currentTarget.value;
    renderContent();
    scheduleSave();
  });
  document.querySelector("#content-search")?.addEventListener("input", (event) => {
    contentSearch = event.currentTarget.value.trim();
    renderContent();
  });
  document.querySelector("#content-list")?.addEventListener("click", (event) => {
    const row = event.target.closest("[data-content-item]");
    if (!row) return;
    const item = contentItems.find((candidate) => candidate.id === row.dataset.contentItem);
    if (item) openContentModal(item);
  });
  document.querySelector("#dashboard-pipeline")?.addEventListener("click", (event) => {
    const stage = event.target.closest("[data-pipeline-status]");
    if (!stage) return;
    openPipelineStage(stage.dataset.pipelineStatus || "", stage.dataset.pipelineKind || "all");
  });
  document.querySelectorAll("[data-dashboard-stat]").forEach((card) => card.addEventListener("click", () => {
    const action = card.dataset.dashboardStat;
    if (action === "audience") {
      navigateTo("audience");
      return;
    }
    openPipelineStage({ published: "published", scheduled: "scheduled", review: "in_review" }[action] || "");
  }));
  document.querySelectorAll("[data-content-close]").forEach((button) => button.addEventListener("click", closeContentModal));
  document.querySelector("#content-modal")?.addEventListener("click", (event) => {
    if (event.target === event.currentTarget) closeContentModal();
  });
  document.querySelector("#content-save")?.addEventListener("click", saveContentItem);
  document.querySelectorAll("[data-account-open]").forEach((button) => button.addEventListener("click", openAccountModal));
  document.querySelectorAll("[data-account-close]").forEach((button) => button.addEventListener("click", closeAccountModal));
  document.querySelector("#account-modal")?.addEventListener("click", (event) => { if (event.target === event.currentTarget) closeAccountModal(); });
  document.querySelector("#account-save")?.addEventListener("click", saveAccount);
  document.querySelectorAll("[data-media-preview-close]").forEach((button) => button.addEventListener("click", closeSocialMediaPreview));
  document.querySelector("#media-preview-modal")?.addEventListener("click", (event) => { if (event.target === event.currentTarget) closeSocialMediaPreview(); });
  document.querySelector("#content-delete")?.addEventListener("click", deleteContentItem);
  document.querySelector("#content-approve-review")?.addEventListener("click", approveContentReview);
  document.querySelector("#content-return-review")?.addEventListener("click", returnContentForRevision);
  document.querySelector("#content-ai-generate")?.addEventListener("click", () => runContentAI("generate"));
  document.querySelector("#content-ai-refine")?.addEventListener("click", () => runContentAI("refine"));
  document.querySelector("#content-item-status")?.addEventListener("change", (event) => {
    document.querySelector("#content-item-scheduled").required = event.currentTarget.value === "scheduled";
  });
  document.querySelector("#strategy-save")?.addEventListener("click", saveStrategy);
  document.querySelector("#strategy-file")?.addEventListener("change", (event) => uploadStrategyFile(event.currentTarget));
  document.querySelectorAll("[data-strategy-field], [data-strategy-tone]").forEach((input) => {
    input.addEventListener("input", scheduleStrategySave);
    input.addEventListener("change", scheduleStrategySave);
  });
  document.querySelectorAll("[data-strategy-goals] button, [data-strategy-topics] button, [data-strategy-mode]").forEach((button) => {
    button.addEventListener("click", () => window.setTimeout(scheduleStrategySave, 0));
  });
  document.querySelector("#strategy-personas")?.addEventListener("click", (event) => {
    const edit = event.target.closest("[data-persona-edit]");
    const remove = event.target.closest("[data-persona-delete]");
    const select = event.target.closest("[data-persona-select]");
    const id = edit?.dataset.personaEdit || remove?.dataset.personaDelete || select?.dataset.personaSelect || "";
    const persona = projectPersonas.find((candidate) => candidate.id === id);
    if (edit) updateProjectPersona(persona);
    else if (remove) deleteProjectPersona(persona);
    else if (select) selectProjectPersona(persona);
  });
  document.querySelector("#strategy-persona-add")?.addEventListener("click", createProjectPersona);
  document.querySelector("#strategy-topic-add")?.addEventListener("click", (event) => {
    event.currentTarget.classList.remove("active");
    const name = window.prompt(currentLanguage === "hr" ? "Nova tema" : "New topic", "");
    if (!name?.trim()) return;
    const button = document.createElement("button");
    button.type = "button";
    button.className = "active";
    button.dataset.strategyValue = name.trim().slice(0, 120);
    button.textContent = button.dataset.strategyValue;
    event.currentTarget.parentElement.insertBefore(button, event.currentTarget);
    scheduleStrategySave();
  });
  document.querySelector("[data-strategy-topics]")?.addEventListener("click", (event) => {
    const button = event.target.closest("button");
    if (!button || button.id === "strategy-topic-add") return;
    if (button.dataset.strategyValue) button.classList.toggle("active");
    scheduleStrategySave();
  });
  document.querySelector("[data-calendar-add]")?.addEventListener("click", () => openCalendarModal());
  document.querySelector("[data-calendar-prev]")?.addEventListener("click", () => {
    calendarCursor = calendarView === "month"
      ? new Date(calendarCursor.getFullYear(), calendarCursor.getMonth() - 1, 1)
      : addDays(calendarCursor, -7);
    loadCalendar().catch((error) => { console.error("Calendar navigation failed", error); showToast("calendarError"); });
  });
  document.querySelector("[data-calendar-next]")?.addEventListener("click", () => {
    calendarCursor = calendarView === "month"
      ? new Date(calendarCursor.getFullYear(), calendarCursor.getMonth() + 1, 1)
      : addDays(calendarCursor, 7);
    loadCalendar().catch((error) => { console.error("Calendar navigation failed", error); showToast("calendarError"); });
  });
  document.querySelectorAll("[data-calendar-view]").forEach((button) => button.addEventListener("click", () => {
    calendarView = button.dataset.calendarView;
    button.parentElement.querySelectorAll("[data-calendar-view]").forEach((choice) => {
      const selected = choice === button;
      choice.classList.toggle("active", selected);
      choice.setAttribute("aria-pressed", String(selected));
    });
    renderCalendar();
    loadCalendar().catch((error) => { console.error("Calendar view failed", error); showToast("calendarError"); });
  }));
  document.querySelector("#calendar-grid")?.addEventListener("click", (event) => {
    const itemButton = event.target.closest("[data-calendar-item]");
    if (itemButton) {
      const item = calendarItems.find((candidate) => candidate.id === itemButton.dataset.calendarItem);
      if (item) {
        openCalendarModal(item);
      } else {
        apiRequest(`/projects/${projectID}/calendar/items/${itemButton.dataset.calendarItem}`)
          .then(openCalendarModal)
          .catch((error) => { console.error("Calendar detail failed", error); showToast("calendarError"); });
      }
      return;
    }
    const slot = event.target.closest("[data-calendar-slot]");
    if (slot) openCalendarModal(null, slot.dataset.calendarSlot);
  });
  document.querySelectorAll("[data-calendar-close]").forEach((button) => button.addEventListener("click", closeCalendarModal));
  document.querySelector("#calendar-modal")?.addEventListener("click", (event) => {
    if (event.target === event.currentTarget) closeCalendarModal();
  });
  document.querySelector("#calendar-save")?.addEventListener("click", saveCalendarItem);
  document.querySelector("#calendar-delete")?.addEventListener("click", deleteCalendarItem);
  document.querySelector("#calendar-item-title")?.addEventListener("input", updateCalendarDetailPreview);
  document.querySelector("#calendar-item-channel")?.addEventListener("change", updateCalendarDetailPreview);
  document.querySelector("#calendar-item-status")?.addEventListener("change", updateCalendarDetailPreview);
  document.querySelectorAll(".language-button").forEach((button) => button.addEventListener("click", () => window.setTimeout(() => {
    renderCalendar();
    renderContent();
    renderStrategyContext();
    renderDashboard();
    renderAutomations();
    renderAudience();
    renderAssistant();
    renderNewsletterDeliveries();
    renderChannelConnections();
    renderTeam();
    renderPlans();
    renderProjectPersonas();
    renderServiceRequests();
    hydrateContentEditors(true);
  }, 0)));
  document.querySelectorAll("[data-editor-command]").forEach((button) => button.addEventListener("click", () => handleEditorCommand(button.dataset.editorCommand)));
  document.querySelectorAll("[data-action-close]").forEach((button) => button.addEventListener("click", closeActionModal));
  document.querySelector("#action-modal")?.addEventListener("click", (event) => {
    if (event.target === event.currentTarget) closeActionModal();
  });

  window.addEventListener("online", () => {
    if (hydrated && projectID) saveState();
    else bootstrap();
  });
  window.addEventListener("offline", () => setAPIStatus("error"));

  applyHonestIntegrationCopy();
  bootstrap();
})();

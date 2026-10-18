"use strict";
// Shared first-party explorer, derived from the tpa-utils grouped browser.
// Inventory decoding is format-specific; rendering never parses APT or HTML.
(async () => {
  const $ = id => document.getElementById(id);
  const list = $("packages"), status = $("status"), search = $("search"), arch = $("architecture"), category = $("category"), includeAll = $("include-all");
  const modal = $("package-modal"), modalTitle = $("modal-title"), modalBody = $("modal-body");
  const PAGE_SIZE = 50; // A hard DOM bound, not infinite append/content-visibility alone.
  let groups = [], candidates = [], architectures = new Set(), base, page = 0, selectedArchitecture = null;
  function element(tag, className, text) {
    const node = document.createElement(tag); node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }
  function field(control, name) {
    const key = Object.keys(control).find(key => key.toLowerCase() === name.toLowerCase());
    return key === undefined ? "" : control[key];
  }
  function artifactURL(pkg) {
    const parts = pkg.filename.split("/");
    if (parts[0] !== "pool" || parts.length < 2 || parts.some(part => !part || part === "." || part === ".." || /[\\\x00-\x1f\x7f]/.test(part))) return null;
    return new URL(parts.map(encodeURIComponent).join("/"), base).href;
  }
  function accepted(pkg) {
    const tokens = search.value.toLowerCase().trim().split(/\s+/).filter(Boolean);
    const requested = [arch.value, ...tokens.filter(token => architectures.has(token))].filter(Boolean);
    return (!category.value || field(pkg.control, "Section") === category.value) && requested.every(value => pkg.architecture === value || (includeAll.checked && pkg.architecture === "all"));
  }
  function size(pkg) { return Number.isSafeInteger(pkg.size) && pkg.size >= 0 ? (pkg.size / 1048576).toFixed(2) + " MiB (" + pkg.size + " bytes)" : "Not provided"; }
  function showMetadata(group) {
    modalTitle.textContent = group.name + " · " + group.version;
    const items = [...group.items].sort((a, b) => a.architecture.localeCompare(b.architecture));
    if (!items.some(pkg => pkg.architecture === selectedArchitecture)) selectedArchitecture = (items.find(accepted) || items[0]).architecture;
    const tabs = element("div", "architecture-tabs"); tabs.setAttribute("role", "tablist"); tabs.setAttribute("aria-label", "Package architecture");
    const panel = element("section", "package-panel"); panel.id = "architecture-panel"; panel.setAttribute("role", "tabpanel"); panel.tabIndex = 0;
    const buttons = [];
    function select(index, focus) {
      const pkg = items[index], control = pkg.control;
      selectedArchitecture = pkg.architecture;
      buttons.forEach((button, i) => { button.setAttribute("aria-selected", String(i === index)); button.tabIndex = i === index ? 0 : -1; });
      panel.setAttribute("aria-labelledby", buttons[index].id); panel.replaceChildren();
      panel.append(element("p", "package-description", (field(control, "Description") || "No description available.").replace(/\n\.\n/g, "\n\n")));
      const fields = element("dl", "package-fields");
      function add(label, value) { if (value !== undefined && value !== "") fields.append(element("dt", "", label), element("dd", "", String(value))); }
      add("Package", pkg.package); add("Version", pkg.version); add("Architecture", pkg.architecture); add("Download size", size(pkg)); add("SHA-256", pkg.sha256 || "Not provided");
      if (field(control, "Installed-Size")) add("Installed size", field(control, "Installed-Size") + " KiB");
      for (const [label, key] of [["Category", "Section"], ["Maintainer", "Maintainer"], ["Homepage", "Homepage"], ["License", "License"], ["Dependencies", "Depends"], ["Pre-dependencies", "Pre-Depends"], ["Recommended", "Recommends"], ["Suggested", "Suggests"]]) add(label, field(control, key));
      panel.append(fields);
      const href = artifactURL(pkg);
      if (href) { const link = element("a", "download primary-download", "Download .deb · " + pkg.architecture); link.href = href; panel.append(link); }
      const advanced = element("details", "advanced");
      advanced.append(element("summary", "", "Advanced · full package metadata"), element("pre", "metadata", JSON.stringify(pkg, null, 2))); panel.append(advanced);
      if (focus) buttons[index].focus();
    }
    items.forEach((pkg, index) => {
      const button = element("button", "architecture-tab", pkg.architecture); button.type = "button"; button.id = "architecture-tab-" + index;
      button.setAttribute("role", "tab"); button.setAttribute("aria-controls", panel.id);
      button.addEventListener("click", () => select(index, false));
      button.addEventListener("keydown", event => {
        let next = index;
        if (event.key === "ArrowRight") next = (index + 1) % items.length;
        else if (event.key === "ArrowLeft") next = (index + items.length - 1) % items.length;
        else if (event.key === "Home") next = 0;
        else if (event.key === "End") next = items.length - 1;
        else return;
        event.preventDefault(); select(next, true);
      });
      buttons.push(button); tabs.append(button);
    });
    modalBody.replaceChildren(tabs, panel); select(items.findIndex(pkg => pkg.architecture === selectedArchitecture), false);
  }
  function card(group) {
    const node = element("article", "package-card"), header = element("header", "package-heading");
    const title = element("h2", "package-name"), button = element("button", "package-open", group.name); button.type = "button";
    button.addEventListener("click", () => { selectedArchitecture = null; showMetadata(group); modal.showModal(); });
    title.append(button); header.append(title, element("span", "version", group.version));
    const variants = element("div", "variants");
    for (const pkg of group.items.filter(accepted)) {
      const href = artifactURL(pkg), badge = element(href ? "a" : "span", "architecture", pkg.architecture);
      if (href) badge.href = href;
      badge.title = "Download " + pkg.architecture + " · " + size(pkg); variants.append(badge);
    }
    node.append(header, element("p", "description", group.description), variants); return node;
  }
  function render() {
    const start = page * PAGE_SIZE, shown = candidates.slice(start, start + PAGE_SIZE);
    // Do not retain a node on every group: traversing pages must not accumulate
    // thousands of detached DOM subtrees either.
    list.replaceChildren(...shown.map(card));
    $("matches").textContent = candidates.length + " matching package versions · showing " + (shown.length ? start + 1 : 0) + "–" + (start + shown.length);
    $("pagination").hidden = candidates.length <= PAGE_SIZE;
    $("previous").disabled = page === 0; $("more").disabled = start + PAGE_SIZE >= candidates.length;
    $("page-number").textContent = "Page " + (page + 1) + " of " + Math.max(1, Math.ceil(candidates.length / PAGE_SIZE));
  }
  function filterAll() {
    const tokens = search.value.toLowerCase().trim().split(/\s+/).filter(Boolean), words = tokens.filter(token => !architectures.has(token));
    candidates = groups.filter(group => words.every(word => group.text.includes(word)) && group.items.some(accepted)); page = 0; render();
  }
  $("filters").addEventListener("submit", event => event.preventDefault());
  search.addEventListener("input", filterAll); arch.addEventListener("change", filterAll); category.addEventListener("change", filterAll); includeAll.addEventListener("change", filterAll);
  $("more").addEventListener("click", () => { if (!$("more").disabled) { page++; render(); } });
  $("previous").addEventListener("click", () => { if (page) { page--; render(); } });
  $("modal-close").addEventListener("click", () => modal.close());
  // Preserve the utility UI's mobile sticky-filter behavior and focus protection.
  if (typeof window !== "undefined") {
    const mobile = window.matchMedia("(max-width: 600px)"), dock = $("search-dock"), marker = $("search-dock-marker"), toggle = $("filter-toggle"), spacer = $("search-dock-spacer");
    let pinned = false, collapsed = false, expandedHeight = 0;
    function collapse(value) {
      if (value === collapsed) { toggle.setAttribute("aria-expanded", String(!value)); return; }
      if (value) { expandedHeight = dock.getBoundingClientRect().height; spacer.style.height = expandedHeight + "px"; }
      collapsed = value; dock.classList.toggle("is-compact", value);
      spacer.style.height = value ? Math.max(0, expandedHeight - dock.getBoundingClientRect().height) + "px" : "0px";
      toggle.setAttribute("aria-expanded", String(!value));
    }
    function updateDock() {
      if (!mobile.matches) { pinned = false; collapse(false); return; }
      const top = marker.getBoundingClientRect().top;
      if (top > 8) { pinned = false; collapse(false); }
      else if (top < -48 && !pinned) { pinned = true; const focus = document.activeElement; if (!dock.contains(focus) || focus === search || focus === toggle) collapse(true); }
    }
    toggle.addEventListener("click", () => collapse(!collapsed)); window.addEventListener("scroll", updateDock, {passive: true}); mobile.addEventListener("change", updateDock); updateDock();
  }
  try {
    if (typeof modal.showModal !== "function") throw new Error("This browser does not support accessible package dialogs");
    const response = await fetch("repository.json", {cache: "no-cache", credentials: "same-origin"});
    if (!response.ok) throw new Error("HTTP " + response.status);
    const data = await response.json(); let packages, baseURL;
    if (data.format === "tpa-repository-index" && data.version === 1 && Array.isArray(data.packages)) {
      // Canonical format is unchanged; this is a presentation adapter only.
      packages = data.packages.map(entry => {
        if (!entry.metadata || !entry.artifact) throw new Error("Invalid canonical inventory entry");
        return {package: field(entry.metadata, "Package"), version: field(entry.metadata, "Version"), architecture: field(entry.metadata, "Architecture"), filename: entry.artifact.filename, size: entry.artifact.size, sha256: entry.artifact.sha256, control: entry.metadata};
      }); baseURL = "./";
    } else if (data.format === "tpa-utils-repository-browser" && data.version === 1 && data.repository && Array.isArray(data.repository.packages)) {
      packages = data.repository.packages; baseURL = data.base_url;
      if (data.repository.signatureVerified !== true) { $("trust-warning").hidden = false; $("trust-warning").textContent = data.repository.signed ? "The signature was not verified against trusted public keys during generation." : "This inventory was generated from an unsigned repository."; }
    } else throw new Error("Unsupported inventory schema");
    base = new URL(baseURL, document.baseURI);
    if (!["https:", "http:"].includes(base.protocol) || base.username || base.password || base.search || base.hash) throw new Error("Unsafe repository URL");
    const grouped = new Map(), identities = new Map(), categories = new Set();
    for (const pkg of packages) {
      if (!pkg || [pkg.package, pkg.version, pkg.architecture, pkg.filename].some(value => typeof value !== "string" || !value) || !pkg.control || typeof pkg.control !== "object" || Object.values(pkg.control).some(value => typeof value !== "string")) throw new Error("Invalid inventory package");
      const identity = JSON.stringify([pkg.package, pkg.version, pkg.architecture]), signature = JSON.stringify([pkg.filename, pkg.size, pkg.sha256, Object.entries(pkg.control).sort(([a], [b]) => a.localeCompare(b))]);
      if (identities.has(identity)) { if (identities.get(identity) !== signature) throw new Error("Conflicting package identity"); continue; } identities.set(identity, signature);
      architectures.add(pkg.architecture); const section = field(pkg.control, "Section"); if (section) categories.add(section);
      const key = JSON.stringify([pkg.package, pkg.version]); let group = grouped.get(key);
      if (!group) { group = {name: pkg.package, version: pkg.version, description: (field(pkg.control, "Description") || "No description available.").split("\n")[0], items: [], text: ""}; grouped.set(key, group); }
      group.items.push(pkg); group.text += " " + [pkg.package, pkg.version, pkg.architecture, pkg.filename, pkg.sha256, ...Object.values(pkg.control)].join(" ").toLowerCase();
    }
    groups = [...grouped.values()];
    for (const [values, select] of [[architectures, arch], [categories, category]]) for (const value of [...values].sort()) { const option = element("option", "", value); option.value = value; select.append(option); }
    $("repository-link").href = base.href; $("repository-link").hidden = false;
    for (const id of ["search", "architecture", "category", "include-all", "filter-toggle"]) $(id).disabled = false;
    filterAll(); status.textContent = identities.size + " package records · " + groups.length + " package versions · at most " + PAGE_SIZE + " cards per page";
  } catch (error) { status.textContent = "Interactive inventory unavailable: " + error.message + ". Static preview and JSON inventory remain available."; }
})();

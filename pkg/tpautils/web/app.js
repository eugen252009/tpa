"use strict";
(async () => {
  const status = document.getElementById("status"), list = document.getElementById("packages");
  const search = document.getElementById("search"), arch = document.getElementById("architecture"), more = document.getElementById("more");
  try {
    const response = await fetch("repository.json", {cache: "no-cache", credentials: "same-origin"});
    if (!response.ok) throw new Error("HTTP " + response.status);
    const data = await response.json();
    if (data.format !== "tpa-utils-repository-browser" || data.version !== 1 || !Array.isArray(data.repository.packages)) throw new Error("Unsupported inventory schema");
    const packages = data.repository.packages;
    const base = new URL(data.base_url, document.baseURI);
    if (!["https:", "http:"].includes(base.protocol) || base.username || base.password) throw new Error("Unsafe repository URL");
    for (const value of [...new Set(packages.map(p => p.architecture))].sort()) {
      const option = document.createElement("option"); option.value = value; option.textContent = value; arch.append(option);
    }
    let limit = 200;
    function render() {
      const query = search.value.toLowerCase();
      const filtered = packages.filter(p => (!arch.value || p.architecture === arch.value) && JSON.stringify(p).toLowerCase().includes(query));
      list.replaceChildren();
      for (const p of filtered.slice(0, limit)) {
        const details = document.createElement("details"), summary = document.createElement("summary"), metadata = document.createElement("pre");
        summary.textContent = p.package + " " + p.version + " (" + p.architecture + ")";
        metadata.textContent = JSON.stringify({metadata: p.control, size: p.size, sha256: p.sha256}, null, 2);
        details.append(summary, metadata);
        const parts = String(p.filename).split("/");
        if (parts[0] === "pool" && parts.length > 1 && parts.every(s => s && s !== "." && s !== ".." && !s.includes("\\"))) {
          const link = document.createElement("a"); link.href = new URL(parts.map(encodeURIComponent).join("/"), base).href;
          link.textContent = "Download " + p.filename; details.append(link);
        }
        list.append(details);
      }
      status.textContent = filtered.length + " of " + packages.length + " packages; showing " + Math.min(limit, filtered.length) + ". Signature verified while reading: " + (data.repository.signatureVerified ? "yes (at generation time)" : "no");
      more.hidden = filtered.length <= limit;
    }
    search.addEventListener("input", () => {limit = 200; render();});
    arch.addEventListener("change", () => {limit = 200; render();});
    more.addEventListener("click", () => {limit += 200; render();});
    render();
  } catch (error) {status.textContent = "Could not load repository metadata: " + error.message + ". Serve this directory over HTTP(S), or inspect repository.json directly.";}
})();

(() => {
  "use strict";

  const root = document.getElementById("hcm-workspace-demo");
  const $ = (id) => root.querySelector("#" + id);
  const main = root.querySelector(".hcm-main");
  const initial = {
    accent: "#006b57",
    company: "Northstar Group",
    shape: "balanced",
    layout: "focus",
    density: "comfortable",
    navigation: "standard",
    navTone: "light",
    latency: 750,
  };
  const state = {
    ...initial,
    page: "home",
    filter: "all",
    search: "",
    busy: false,
    modal: null,
    modalStep: 1,
    modalTrigger: null,
    lastWork: null,
    selectedWork: "promotion",
    selectedPerson: "jordan",
    toastTimer: null,
    searchTimer: null,
    peopleFilter: "all",
    orgFilter: false,
    notificationsRead: false,
  };

  const work = [
    {
      id: "promotion",
      name: "Jordan Lee",
      initials: "JL",
      title: "Promotion review",
      meta: "Senior Analyst → Senior Manager",
      status: "Awaiting review",
      kind: "review",
      due: "Due today",
      tone: "hcm-wait",
    },
    {
      id: "coverage",
      name: "Avery Patel",
      initials: "AP",
      title: "Coverage decision",
      meta: "Cross-team support · begins Sep 8",
      status: "Awaiting review",
      kind: "review",
      due: "Due Sep 7",
      tone: "hcm-wait",
    },
    {
      id: "pto",
      name: "Noah Williams",
      initials: "NW",
      title: "Time off request",
      meta: "Sep 14–15 · 16 hours",
      status: "Awaiting review",
      kind: "review",
      due: "Due Sep 10",
      tone: "hcm-wait",
    },
    {
      id: "recruiting",
      name: "Sam Rivera",
      initials: "SR",
      title: "Interview scheduling",
      meta: "HR Operations Lead · REQ-301",
      status: "Waiting on panel",
      kind: "waiting",
      due: "Sep 10",
      tone: "",
    },
  ];
  const people = [
    {
      id: "maya",
      name: "Maya Chen",
      initials: "MC",
      role: "Chief People Officer",
      team: "People Operations",
      manager: "Executive team",
      location: "Chicago, IL",
    },
    {
      id: "alex",
      name: "Alex Morgan",
      initials: "AM",
      role: "VP, Strategy",
      team: "Strategy",
      manager: "Maya Chen",
      location: "New York, NY",
    },
    {
      id: "jordan",
      name: "Jordan Lee",
      initials: "JL",
      role: "Senior Analyst",
      team: "Strategy",
      manager: "Alex Morgan",
      location: "New York, NY",
    },
    {
      id: "avery",
      name: "Avery Patel",
      initials: "AP",
      role: "Product Designer",
      team: "Product",
      manager: "Elena Ruiz",
      location: "Toronto, ON",
    },
    {
      id: "noah",
      name: "Noah Williams",
      initials: "NW",
      role: "Payroll Specialist",
      team: "People Operations",
      manager: "Maya Chen",
      location: "Chicago, IL",
    },
    {
      id: "elena",
      name: "Elena Ruiz",
      initials: "ER",
      role: "VP, Product",
      team: "Product",
      manager: "Not shown",
      location: "San Francisco, CA",
    },
  ];
  const schemes = {
    standard: [
      ["", "home", "Home", "house"],
      ["", "work", "My Work", "clipboard-list"],
      ["", "people", "People", "users"],
      ["", "organization", "Organization", "network"],
      ["", "insights", "Insights", "chart-no-axes-combined"],
      ["", "admin", "Admin", "shield-check"],
    ],
    department: [
      ["WORKSPACE", "home", "Overview", "house"],
      ["", "work", "Requests", "clipboard-list"],
      ["PEOPLE", "people", "Directory", "users"],
      ["", "organization", "Team structure", "network"],
      ["OPERATIONS", "insights", "Reports", "chart-no-axes-combined"],
      ["", "admin", "Admin", "shield-check"],
    ],
    daily: [
      ["MY DAY", "home", "Start", "house"],
      ["", "work", "Tasks", "clipboard-list"],
      ["MY TEAM", "people", "Team", "users"],
      ["", "organization", "Coverage", "network"],
      ["INSIGHTS", "insights", "Reports", "chart-no-axes-combined"],
      ["TOOLS", "admin", "Admin", "shield-check"],
    ],
  };
  const pageCopy = {
    home: ["Good morning, Maya.", "Review requests and keep your team moving."],
    work: ["My Work", "Requests and decisions that need your attention."],
    people: ["People", "Find the right context for the people you support."],
    organization: ["Organization", "Understand reporting lines, teams, and coverage."],
    insights: [
      "Insights",
      "See workforce movement without losing the underlying definitions.",
    ],
    help: ["Help center", "Guidance for the work you are doing now."],
    settings: [
      "Settings",
      "Manage your personal experience and customer presentation.",
    ],
    admin: [
      "Admin",
      "Configure governed experiences and review operational readiness.",
    ],
  };

  const esc = (value) =>
    String(value).replace(
      /[&<>"']/g,
      (character) =>
        ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
          character
        ],
    );
  const rgb = (hexValue) =>
    hexValue
      .slice(1)
      .match(/../g)
      .map((part) => parseInt(part, 16));
  const luminance = (hexValue) =>
    rgb(hexValue)
      .map((channel) => channel / 255)
      .map((channel) =>
        channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4,
      )
      .reduce(
        (sum, channel, index) => sum + channel * [0.2126, 0.7152, 0.0722][index],
        0,
      );
  const contrast = (first, second) =>
    (Math.max(luminance(first), luminance(second)) + 0.05) /
    (Math.min(luminance(first), luminance(second)) + 0.05);
  const hex = (channels) =>
    "#" +
    channels
      .map((channel) => Math.round(channel).toString(16).padStart(2, "0"))
      .join("");
  const sleep = (milliseconds) =>
    new Promise((resolve) => setTimeout(resolve, milliseconds));
  const icons = () => {
    if (globalThis.lucide)
      lucide.createIcons({ attrs: { width: 20, height: 20, "stroke-width": 1.65 } });
  };

  function toast(title, detail = "") {
    clearTimeout(state.toastTimer);
    $("hcm-toast-region").innerHTML =
      '<div class="hcm-toast"><i data-lucide="circle-check" aria-hidden="true"></i><div><strong>' +
      esc(title) +
      "</strong>" +
      (detail ? "<p>" + esc(detail) + "</p>" : "") +
      "</div></div>";
    icons();
    state.toastTimer = setTimeout(() => {
      $("hcm-toast-region").innerHTML = "";
    }, 4300);
  }

  function announce(text) {
    $("hcm-feedback").hidden = false;
    $("hcm-feedback").textContent = text;
  }

  function loadingMarkup(label = "Loading the latest authorized data") {
    return (
      '<div class="hcm-loading-shell" role="status" aria-label="' +
      esc(label) +
      '"><div class="hcm-skeleton hcm-skeleton-title"></div><div class="hcm-skeleton hcm-skeleton-line" style="width:58%"></div><div class="hcm-route-grid"><div class="hcm-skeleton hcm-skeleton-panel hcm-span-8"></div><div class="hcm-skeleton hcm-skeleton-panel hcm-span-4"></div></div></div>'
    );
  }

  function buttonPending(button, pending, label = "Loading") {
    if (!button) return;
    if (pending) {
      button.dataset.pending = "true";
      button.dataset.original = button.innerHTML;
      button.disabled = true;
      button.innerHTML =
        '<span class="hcm-spinner" aria-hidden="true"></span>' + esc(label);
    } else {
      button.disabled = false;
      button.removeAttribute("data-pending");
      if (button.dataset.original) button.innerHTML = button.dataset.original;
      delete button.dataset.original;
      icons();
    }
  }

  async function simulate(action, options = {}) {
    if (state.busy) return;
    const {
      button = null,
      label = "Loading",
      delay = state.latency,
      route = false,
    } = options;
    state.busy = true;
    root.dataset.busy = "true";
    main.setAttribute("aria-busy", "true");
    $("hcm-route-progress").hidden = false;
    buttonPending(button, true, label);
    if (route) {
      showOnly("route");
      $("hcm-route-view").innerHTML = loadingMarkup(label);
    }
    await sleep(Number(delay));
    try {
      await action();
    } finally {
      state.busy = false;
      delete root.dataset.busy;
      main.removeAttribute("aria-busy");
      $("hcm-route-progress").hidden = true;
      buttonPending(button, false);
    }
  }

  function applyTheme() {
    const accent = state.accent;
    const onAccent =
      contrast(accent, "#ffffff") >= 4.5
        ? "#ffffff"
        : contrast(accent, "#102238") >= 4.5
          ? "#102238"
          : "#000000";
    const soft = hex(rgb(accent).map((channel) => channel * 0.09 + 255 * 0.91));
    let link = accent;
    for (let index = 0; index < 30 && contrast(link, soft) < 4.5; index += 1)
      link = hex(rgb(link).map((channel) => channel * 0.88));
    const hover =
      onAccent === "#ffffff"
        ? hex(rgb(accent).map((channel) => channel * 0.92))
        : hex(rgb(accent).map((channel) => channel * 0.94 + 255 * 0.06));
    root.style.setProperty("--hcm-accent", accent);
    root.style.setProperty("--hcm-accent-hover", hover);
    root.style.setProperty("--hcm-on-accent", onAccent);
    root.style.setProperty("--hcm-link", link);
    root.style.setProperty("--hcm-soft", soft);
    const radii = { square: [4, 8], balanced: [8, 12], soft: [12, 16] }[state.shape];
    root.style.setProperty("--hcm-radius", radii[0] + "px");
    root.style.setProperty("--hcm-panel", radii[1] + "px");
    root.style.setProperty("--hcm-row", state.density === "compact" ? "10px" : "17px");
    root.dataset.layout = state.layout;
    root.dataset.navTone = state.navTone;
    const company = state.company.trim() || "Your company";
    $("hcm-tenant-name").textContent = company;
    $("hcm-brand-name").textContent =
      company === "Northstar Group" ? "HCM Next" : company;
    $("hcm-theme-check").textContent =
      "Primary label contrast " +
      contrast(accent, onAccent).toFixed(2) +
      ":1 · Status colors stay independent · Local preview";
    root
      .querySelectorAll("[data-accent]")
      .forEach((button) =>
        button.setAttribute("aria-pressed", String(button.dataset.accent === accent)),
      );
    const layout = $("hcm-home-layout");
    const attention = $("hcm-attention");
    const aside = $("hcm-aside-stack");
    const preview = $("hcm-work-preview");
    if (state.layout === "team") layout.append(aside, attention, preview);
    else layout.append(attention, aside, preview);
    renderNav();
    icons();
  }

  function renderNav() {
    let markup = "";
    for (const [group, id, label, icon] of schemes[state.navigation]) {
      if (group) markup += '<div class="hcm-nav-group">' + group + "</div>";
      markup +=
        '<button class="hcm-nav-button" data-page="' +
        id +
        '"' +
        (state.page === id ? ' aria-current="page"' : "") +
        '><i data-lucide="' +
        icon +
        '" aria-hidden="true"></i>' +
        label +
        (id === "work"
          ? '<span class="hcm-nav-count">' +
            work.filter((item) => item.kind !== "complete").length +
            "</span>"
          : "") +
        "</button>";
    }
    $("hcm-navigation-items").innerHTML = markup;
  }

  function renderWork() {
    const active = work.filter((item) => item.kind !== "complete");
    const filtered = work.filter(
      (item) =>
        (state.filter === "complete"
          ? item.kind === "complete"
          : item.kind !== "complete" &&
            (state.filter === "all" || item.kind === state.filter)) &&
        (item.title + " " + item.name + " " + item.meta)
          .toLowerCase()
          .includes(state.search),
    );
    $("hcm-queue-count").textContent =
      filtered.length + " " + (filtered.length === 1 ? "item" : "items");
    $("hcm-summary-open").textContent = active.length;
    $("hcm-summary-review").textContent = active.filter(
      (item) => item.kind === "review",
    ).length;
    $("hcm-summary-waiting").textContent = active.filter(
      (item) => item.kind === "waiting",
    ).length;
    $("hcm-summary-due").textContent = active.filter(
      (item) => item.due === "Due today",
    ).length;
    $("hcm-work-list").innerHTML = filtered.length
      ? filtered
          .map(
            (item) =>
              '<button class="hcm-row' +
              (state.page === "work" && item.id === state.selectedWork
                ? " hcm-row-selected"
                : "") +
              '" data-work="' +
              item.id +
              '" aria-pressed="' +
              (state.page === "work" && item.id === state.selectedWork) +
              '"><span class="hcm-avatar">' +
              item.initials +
              '</span><span class="hcm-row-main"><span class="hcm-row-title">' +
              item.title +
              '</span><span class="hcm-row-meta">' +
              item.name +
              '</span><span class="hcm-row-meta">' +
              item.meta +
              '</span></span><span class="hcm-row-end"><span class="hcm-status ' +
              item.tone +
              '">' +
              item.status +
              "</span><span>" +
              item.due +
              '</span></span><i class="hcm-row-open" data-lucide="chevron-right" aria-hidden="true"></i></button>',
          )
          .join("")
      : '<div class="hcm-empty">No matching work. Try another search or filter.</div>';
    root
      .querySelectorAll("[data-filter]")
      .forEach((button) =>
        button.setAttribute(
          "aria-pressed",
          String(button.dataset.filter === state.filter),
        ),
      );
    icons();
  }

  function renderWorkPreview() {
    const item = work.find((entry) => entry.id === state.selectedWork) || work[0];
    const preview = $("hcm-work-preview");
    preview.innerHTML =
      '<div class="hcm-preview-head"><div class="hcm-preview-icon"><i data-lucide="' +
      (item.id === "promotion" ? "trending-up" : "clipboard-check") +
      '" aria-hidden="true"></i></div><div><span class="hcm-small">Selected assignment</span><h2>' +
      esc(item.title) +
      '</h2><p class="hcm-small">' +
      esc(item.name) +
      " · " +
      esc(item.meta) +
      '</p></div><span class="hcm-status ' +
      item.tone +
      '">' +
      esc(item.status) +
      '</span></div><div class="hcm-preview-summary"><h3>Proposal summary</h3>' +
      (item.id === "promotion"
        ? '<div><span>Effective date</span><strong>Sep 15, 2026</strong></div><div><span>Annual base</span><strong>USD 92,000 → 112,000</strong></div><div><span>Policy checks</span><strong class="hcm-positive">3 checks passed</strong></div><div><span>Conflicts</span><strong>None found</strong></div>'
        : "<div><span>Assigned to</span><strong>Maya Chen</strong></div><div><span>Due</span><strong>" +
          esc(item.due) +
          "</strong></div><div><span>Scope</span><strong>North America</strong></div>") +
      '</div><div class="hcm-workflow-line"><span class="is-complete"><i data-lucide="file-text" aria-hidden="true"></i><small>Draft</small></span><i></i><span class="is-complete"><i data-lucide="circle-check" aria-hidden="true"></i><small>Simulated</small></span><i></i><span class="is-current"><i data-lucide="user-check" aria-hidden="true"></i><small>Review</small></span></div><div class="hcm-preview-actions"><button class="hcm-button hcm-primary" data-open-work="' +
      item.id +
      '">Review proposal</button><button class="hcm-button" data-open-work="' +
      item.id +
      '">Open full page</button></div>';
    icons();
  }

  function renderPeople() {
    const filtered = people.filter(
      (person) =>
        (state.peopleFilter === "all" || person.team === state.peopleFilter) &&
        (person.name + " " + person.role + " " + person.team + " " + person.location)
          .toLowerCase()
          .includes(state.search),
    );
    const selected =
      filtered.find((person) => person.id === state.selectedPerson) || filtered[0];
    if (selected) state.selectedPerson = selected.id;
    $("hcm-directory").innerHTML =
      '<div class="hcm-directory-toolbar"><div><strong>' +
      filtered.length +
      (filtered.length === 1 ? " person" : " people") +
      '</strong><span class="hcm-small">Authorized worker directory · North America</span></div><div class="hcm-flex"><button class="hcm-button" data-route-action="people-filter"><i data-lucide="list-filter" aria-hidden="true"></i>' +
      (state.peopleFilter === "all" ? "Show Product team" : "Clear team filter") +
      '</button><button class="hcm-button hcm-primary" data-action="promotion">Start an action<i data-lucide="chevron-down" aria-hidden="true"></i></button></div></div><div class="hcm-people-workspace"><div class="hcm-people-table"><div class="hcm-people-columns"><span>Person</span><span>Role</span><span>Team</span><span>Manager</span><span>Location</span></div>' +
      (filtered.length
        ? filtered
            .map(
              (person) =>
                '<button class="hcm-people-row' +
                (person.id === state.selectedPerson ? " is-selected" : "") +
                '" data-person-select="' +
                person.id +
                '" aria-pressed="' +
                (person.id === state.selectedPerson) +
                '"><span class="hcm-person-cell"><span class="hcm-avatar">' +
                person.initials +
                "</span><strong>" +
                person.name +
                "</strong></span><span>" +
                person.role +
                "</span><span>" +
                person.team +
                "</span><span>" +
                person.manager +
                "</span><span>" +
                person.location.replace(/, [A-Z]{2}$/, "") +
                "</span></button>",
            )
            .join("")
        : '<div class="hcm-empty">No people match this search and team filter.</div>') +
      '</div><aside class="hcm-person-context">' +
      (selected
        ? '<div class="hcm-person-context-head"><span class="hcm-avatar">' +
          selected.initials +
          "</span><div><h2>" +
          selected.name +
          '</h2><span class="hcm-small">' +
          selected.role +
          "</span></div></div><dl><div><dt>Manager</dt><dd>" +
          selected.manager +
          "</dd></div><div><dt>Team</dt><dd>" +
          selected.team +
          "</dd></div><div><dt>Location</dt><dd>" +
          selected.location +
          '</dd></div><div><dt>Status</dt><dd><span class="hcm-positive">● Active</span></dd></div></dl>' +
          (selected.id === "jordan"
            ? '<div class="hcm-upcoming"><span class="hcm-small">Upcoming change</span><strong>Promotion review</strong><span class="hcm-status hcm-wait">Awaiting review</span><small>Effective Sep 15, 2026</small></div>'
            : "") +
          '<button class="hcm-button" data-person-modal="' +
          selected.id +
          '">View full profile</button><button class="hcm-button hcm-primary" data-action="promotion">Start an action<i data-lucide="arrow-right" aria-hidden="true"></i></button>'
        : '<div class="hcm-empty">Select a person to see authorized context.</div>') +
      "</aside></div>";
    icons();
  }

  function showOnly(view) {
    $("hcm-home-view").hidden = view !== "home";
    $("hcm-people-view").hidden = view !== "people";
    $("hcm-route-view").hidden = view !== "route";
    $("hcm-detail-view").hidden = view !== "detail";
  }

  function setHeading(id) {
    const copy = pageCopy[id] || pageCopy.home;
    $("hcm-page-title").textContent = copy[0];
    $("hcm-page-subtitle").textContent = copy[1];
  }

  function renderRoute(id) {
    let markup = "";
    if (id === "organization") {
      const productBranch = state.orgFilter
        ? ""
        : '<div class="hcm-org-branch"><button class="hcm-org-node hcm-org-manager" data-person="elena"><span class="hcm-avatar">ER</span><span><strong>Elena Ruiz</strong><small>VP, Product · 18 reports</small></span><i data-lucide="arrow-up-right" aria-hidden="true"></i></button><div class="hcm-org-children"><button class="hcm-org-node" data-person="avery"><span class="hcm-avatar">AP</span><span><strong>Avery Patel</strong><small>Product Designer · Toronto</small></span></button></div></div>';
      markup =
        '<div class="hcm-route-grid hcm-route-enter"><section class="hcm-surface hcm-route-card hcm-span-full"><div class="hcm-route-toolbar"><div><h2>Reporting structure</h2><p class="hcm-small">' +
        (state.orgFilter
          ? "Strategy team · 1 branch shown"
          : "People, Strategy, and Product · 3 branches shown") +
        '</p></div><div class="hcm-flex"><button class="hcm-button" data-route-action="org-date"><i data-lucide="calendar-days" aria-hidden="true"></i>As of Sep 5, 2026</button><button class="hcm-segment is-active" data-route-action="org-view">Chart</button><button class="hcm-segment" data-route-action="org-view">Outline</button><button class="hcm-button" data-route-action="org-filter"><i data-lucide="list-filter" aria-hidden="true"></i>' +
        (state.orgFilter ? "Clear team filter" : "Show Strategy only") +
        '</button><button class="hcm-button" data-route-action="org-changes">View changes</button></div></div><div class="hcm-org-tree"><button class="hcm-org-node hcm-org-leader" data-person="maya"><span class="hcm-avatar">MC</span><span><strong>Maya Chen</strong><small>Chief People Officer · North America</small></span><i data-lucide="arrow-up-right" aria-hidden="true"></i></button><div class="hcm-org-branches"><div class="hcm-org-branch"><button class="hcm-org-node hcm-org-manager" data-person="alex"><span class="hcm-avatar">AM</span><span><strong>Alex Morgan</strong><small>VP, Strategy · 9 reports</small></span><i data-lucide="arrow-up-right" aria-hidden="true"></i></button><div class="hcm-org-children"><button class="hcm-org-node" data-person="jordan"><span class="hcm-avatar">JL</span><span><strong>Jordan Lee</strong><small>Senior Analyst · New York</small></span></button></div></div>' +
        productBranch +
        '<div class="hcm-org-branch"><button class="hcm-org-node hcm-org-manager" data-person="noah"><span class="hcm-avatar">NW</span><span><strong>Noah Williams</strong><small>Payroll Specialist · Chicago</small></span><i data-lucide="arrow-up-right" aria-hidden="true"></i></button></div></div></div></section><aside class="hcm-surface hcm-route-card hcm-span-full hcm-coverage-strip"><div><h2>Coverage outlook</h2><p class="hcm-small">Avery’s coverage begins September 8. Private absence details remain hidden.</p></div><div class="hcm-coverage-stat"><strong>1</strong><span>open coverage decision</span></div><div class="hcm-coverage-stat"><strong>0</strong><span>unfilled manager roles</span></div><button class="hcm-button" data-work="coverage">Review coverage</button></aside></div>';
    } else if (id === "insights") {
      markup = `<div class="hcm-route-enter">
        <div class="hcm-insights-toolbar"><div class="hcm-flex"><button class="hcm-button" data-route-action="report-filter">Region · North America<i data-lucide="chevron-down"></i></button><button class="hcm-button" data-route-action="report-filter">Last 90 days<i data-lucide="chevron-down"></i></button><span class="hcm-small"><i data-lucide="circle-check"></i> Data current · 8:42 AM</span></div><button class="hcm-button" data-route-action="browse-reports">Browse reports</button></div>
        <div class="hcm-metric-row hcm-insight-metrics"><div class="hcm-metric"><span class="hcm-small">Completed changes</span><strong>18</strong><span class="hcm-metric-note hcm-positive">+12% vs prior period</span></div><div class="hcm-metric"><span class="hcm-small">Median completion</span><strong>2.4d</strong><span class="hcm-metric-note">0.6 days faster</span></div><div class="hcm-metric"><span class="hcm-small">Needs reconciliation</span><strong>2</strong><span class="hcm-metric-note">Owner action required</span></div></div>
        <div class="hcm-route-grid"><section class="hcm-surface hcm-route-card hcm-span-8"><div class="hcm-section-head hcm-no-pad"><div><h2>Workforce change volume</h2><p class="hcm-small">Status by week · authorized scope</p></div><button class="hcm-button" data-route-action="export-report"><i data-lucide="download"></i>Export</button></div><div class="hcm-stacked-chart" role="img" aria-label="Weekly workforce changes completed, in progress, and needing reconciliation"><div><span>Aug 10</span><b style="--done:58%;--progress:27%;--issue:8%"></b><strong>21</strong></div><div><span>Aug 17</span><b style="--done:67%;--progress:20%;--issue:6%"></b><strong>24</strong></div><div><span>Aug 24</span><b style="--done:72%;--progress:17%;--issue:5%"></b><strong>28</strong></div><div><span>Aug 31</span><b style="--done:61%;--progress:24%;--issue:9%"></b><strong>26</strong></div></div><div class="hcm-chart-legend"><span><i class="is-done"></i>Completed</span><span><i class="is-progress"></i>In progress</span><span><i class="is-issue"></i>Reconciliation</span></div><p class="hcm-definition"><strong>Definition:</strong> governed worker changes grouped by submission week. Canceled proposals excluded.</p></section><aside class="hcm-surface hcm-route-card hcm-span-4"><div class="hcm-section-head hcm-no-pad"><h2>Needs attention</h2><span class="hcm-status hcm-wait">2 items</span></div><button class="hcm-insight-alert" data-route-action="review-transactions"><strong>2 changes need reconciliation</strong><span>Missing owner confirmation</span><i data-lucide="chevron-right"></i></button><button class="hcm-insight-alert" data-route-action="review-transactions"><strong>6 changes are in progress</strong><span>2 due in the next 48 hours</span><i data-lucide="chevron-right"></i></button></aside></div>
        <section class="hcm-surface hcm-report-list"><div class="hcm-section-head"><div><h2>Certified reports</h2><p class="hcm-small">Definitions reviewed by People Analytics</p></div><button class="hcm-quiet" data-route-action="browse-reports">View all</button></div><button data-route-action="report-open"><span><strong>Workforce movement</strong><small>Hires, transfers, promotions, and exits</small></span><span class="hcm-status hcm-success">Certified</span><span>Updated today</span><i data-lucide="chevron-right"></i></button><button data-route-action="report-open"><span><strong>Decision turnaround</strong><small>Assignment-to-decision cycle time</small></span><span class="hcm-status hcm-success">Certified</span><span>Updated today</span><i data-lucide="chevron-right"></i></button></section>
      </div>`;
    } else if (id === "help") {
      markup =
        '<div class="hcm-route-grid hcm-route-enter"><section class="hcm-surface hcm-route-card hcm-span-7"><h2>Popular guidance</h2><div class="hcm-actions" style="padding:12px 0 0"><button class="hcm-quiet" data-route-action="help-promotion"><i data-lucide="trending-up" aria-hidden="true"></i>Run a promotion review<i data-lucide="chevron-right" aria-hidden="true"></i></button><button class="hcm-quiet" data-route-action="help-customize"><i data-lucide="palette" aria-hidden="true"></i>Customize a workspace<i data-lucide="chevron-right" aria-hidden="true"></i></button><button class="hcm-quiet" data-route-action="help-permissions"><i data-lucide="shield-check" aria-hidden="true"></i>Understand access and scope<i data-lucide="chevron-right" aria-hidden="true"></i></button></div></section><aside class="hcm-surface hcm-route-card hcm-span-5"><h2>Still need help?</h2><p class="hcm-small" style="margin-top:8px">Send a demo support request. Nothing leaves this browser.</p><button class="hcm-button hcm-primary" style="margin-top:20px" data-route-action="contact-support">Contact support</button></aside></div>';
    } else if (id === "settings") {
      markup = `<div class="hcm-settings-shell hcm-surface hcm-route-enter"><nav class="hcm-settings-nav" aria-label="Settings sections"><span class="hcm-small">PERSONAL</span><button data-route-action="settings-section">Profile</button><button class="is-active" data-route-action="settings-section">Preferences</button><button data-route-action="settings-section">Notifications</button><button data-route-action="settings-section">Accessibility</button><span class="hcm-small">SECURITY</span><button data-route-action="settings-section">Sessions & devices</button><button data-route-action="settings-section">Delegated access</button></nav><section class="hcm-settings-form"><div><h2>Preferences</h2><p class="hcm-small">Choose how HCM Next looks and behaves for you.</p></div><fieldset><legend>Density</legend><div class="hcm-choice-row"><button type="button" class="hcm-choice" data-setting-choice="comfortable" aria-pressed="${state.density === "comfortable"}"><strong>Comfortable</strong><span class="hcm-small">More breathing room</span></button><button type="button" class="hcm-choice" data-setting-choice="compact" aria-pressed="${state.density === "compact"}"><strong>Compact</strong><span class="hcm-small">More work per view</span></button></div></fieldset><fieldset><legend>Appearance</legend><div class="hcm-choice-row"><button type="button" class="hcm-choice" data-route-action="theme-choice" aria-pressed="true"><strong>Light</strong><span class="hcm-small">Clear, warm surfaces</span></button><button type="button" class="hcm-choice" data-route-action="theme-choice" aria-pressed="false"><strong>System</strong><span class="hcm-small">Follow your device</span></button></div><label class="hcm-check"><input type="checkbox" /> Reduce motion where possible</label></fieldset><div class="hcm-form-grid"><label class="hcm-field">Language<select><option>English (US)</option><option>French (Canada)</option></select></label><label class="hcm-field">Time zone<select><option>Eastern Time</option><option>Central Time</option></select></label><label class="hcm-field">Default scope<select><option>People Operations · North America</option><option>My team</option></select></label><label class="hcm-field">Start page<select><option>Home</option><option>My Work</option></select></label></div><button class="hcm-button hcm-primary" data-route-action="save-preferences">Save preferences</button></section><aside class="hcm-settings-context"><h2>Access context</h2><p class="hcm-small">Your current security posture</p><dl><div><dt>Organization</dt><dd>Northstar Group</dd></div><div><dt>Acting as</dt><dd>Yourself</dd></div><div><dt>Data scope</dt><dd>People Operations<br />North America</dd></div><div><dt>Authentication</dt><dd class="hcm-positive">MFA verified</dd></div></dl><div class="hcm-access-note"><i data-lucide="shield-check"></i><p>Changing preferences does not change permissions.</p></div><button class="hcm-button" data-route-action="open-customizer"><i data-lucide="sliders-horizontal"></i>Workspace controls</button></aside></div>`;
    } else if (id === "admin") {
      markup = `<div class="hcm-admin-grid hcm-route-enter"><section class="hcm-surface hcm-admin-hero"><div><span class="hcm-small">CUSTOMER CONFIGURATION</span><h2>Shape a governed experience</h2><p>Organize pages, workflows, and branding without weakening runtime authorization.</p></div><button class="hcm-button hcm-primary" data-route-action="admin-experience">Open Experience Studio</button></section><section class="hcm-admin-card"><span class="hcm-admin-icon"><i data-lucide="layout-dashboard"></i></span><div><h3>Experience Studio</h3><p>Compose pages and navigation for specific audiences.</p></div><strong class="hcm-positive">Published</strong><button class="hcm-link-button" data-route-action="admin-experience">Configure <i data-lucide="arrow-right"></i></button></section><section class="hcm-admin-card"><span class="hcm-admin-icon"><i data-lucide="workflow"></i></span><div><h3>Workflow catalog</h3><p>Review default and customer-authored workflows.</p></div><strong>12 active</strong><button class="hcm-link-button" data-route-action="admin-workflows">Review catalog <i data-lucide="arrow-right"></i></button></section><section class="hcm-admin-card"><span class="hcm-admin-icon"><i data-lucide="shield-check"></i></span><div><h3>Access policies</h3><p>Inspect audiences, field access, and governed actions.</p></div><strong class="hcm-positive">No conflicts</strong><button class="hcm-link-button" data-route-action="admin-policies">Inspect policies <i data-lucide="arrow-right"></i></button></section><section class="hcm-admin-card"><span class="hcm-admin-icon"><i data-lucide="plug-zap"></i></span><div><h3>Integration health</h3><p>See synchronization and outbound delivery status.</p></div><strong class="hcm-positive">Healthy</strong><button class="hcm-link-button" data-route-action="admin-integrations">View health <i data-lucide="arrow-right"></i></button></section><section class="hcm-surface hcm-admin-audit"><div><h2>Recent configuration activity</h2><p class="hcm-small">Changes are attributed and auditable.</p></div><span>Navigation published by Maya Chen</span><time>Today · 9:18 AM</time><button class="hcm-button" data-route-action="admin-audit">View audit log</button></section></div>`;
    }
    $("hcm-route-view").innerHTML = markup;
    icons();
  }

  function renderPage(id) {
    state.page = id;
    root.dataset.page = id;
    root.dataset.menu = "closed";
    $("hcm-menu").setAttribute("aria-expanded", "false");
    setHeading(id);
    if (id === "home" || id === "work") {
      showOnly("home");
      $("hcm-aside-stack").hidden = id === "work";
      $("hcm-work-preview").hidden = id !== "work";
      $("hcm-work-overview").hidden = true;
      $("hcm-work-footer").hidden = id === "work";
      $("hcm-recent").hidden = id === "work";
      $("hcm-news").hidden = id === "work";
      $("hcm-queue-title").textContent =
        id === "work" ? "Assigned to you" : "Needs your attention";
      renderWork();
      if (id === "work") renderWorkPreview();
    } else if (id === "people") {
      showOnly("people");
      renderPeople();
    } else {
      showOnly("route");
      renderRoute(id);
    }
    renderNav();
    icons();
    main.focus({ preventScroll: true });
  }

  async function navigate(id, button) {
    await simulate(() => renderPage(id), {
      button,
      label: "Loading " + (pageCopy[id]?.[0] || "page"),
      route: true,
    });
  }

  function renderDetail(id) {
    const item = work.find((entry) => entry.id === id);
    if (!item) return;
    state.lastWork = id;
    showOnly("detail");
    $("hcm-page-title").textContent = item.title;
    $("hcm-page-subtitle").textContent =
      "Review the context before choosing a next step.";
    let content =
      '<div class="hcm-detail-heading"><span class="hcm-avatar">' +
      item.initials +
      '</span><div class="hcm-row-main"><h2>' +
      item.name +
      '</h2><p class="hcm-small">' +
      item.title +
      '</p></div><div class="hcm-detail-status"><span class="hcm-status ' +
      item.tone +
      '">' +
      item.status +
      '</span><span class="hcm-small">' +
      item.due +
      "</span></div></div>";
    content +=
      '<div class="hcm-decision-bar"><div><strong>Ready for your decision?</strong><span class="hcm-small">Review the outcome before the local preview saves it.</span></div><button class="hcm-button hcm-primary" data-preview-decision>Review decision<i data-lucide="arrow-right" aria-hidden="true"></i></button></div>';
    if (id === "promotion")
      content +=
        '<div class="hcm-small">PR-1042 · Revision 3 · Requested by Alex Morgan</div><div class="hcm-comparison"><div><h3>Current</h3><div class="hcm-fact"><small>Job</small>Senior Analyst</div><div class="hcm-fact"><small>Annual base</small>USD 92,000</div><div class="hcm-fact"><small>Manager</small>Alex Morgan</div></div><div><h3>Proposed</h3><div class="hcm-fact"><small>Job</small>Senior Manager</div><div class="hcm-fact"><small>Annual base</small>USD 112,000</div><div class="hcm-fact"><small>Effective date</small>Sep 15, 2026</div></div></div><h3>Evidence for your review</h3><ul class="hcm-evidence"><li><i data-lucide="circle-check" aria-hidden="true"></i>Compensation guidelines checked</li><li><i data-lucide="circle-check" aria-hidden="true"></i>Pay range checked</li><li><i data-lucide="circle-check" aria-hidden="true"></i>Budget availability checked</li></ul><p class="hcm-small">HR and compensation reviews are required. This proposal has not changed the worker record.</p>';
    else if (id === "pto")
      content +=
        '<div class="hcm-comparison"><div><h3>Request</h3><div class="hcm-fact"><small>Dates</small>Sep 14–15, 2026</div><div class="hcm-fact"><small>Time requested</small>16 hours</div></div><div><h3>Balance</h3><div class="hcm-fact"><small>Available</small>40 hours</div><div class="hcm-fact"><small>If approved</small>24 hours</div></div></div><p class="hcm-small">Review the balance and team coverage before a decision.</p>';
    else if (id === "coverage")
      content +=
        '<div class="hcm-comparison"><div><h3>Coverage needed</h3><div class="hcm-fact"><small>Starts</small>Sep 8, 2026</div></div><div><h3>Coordination</h3><div class="hcm-fact"><small>Worker manager</small>Elena Ruiz</div></div></div><p class="hcm-small">Only coverage information is shown. Private absence details are not needed for this decision.</p>';
    else
      content +=
        '<div class="hcm-comparison"><div><h3>Interview</h3><div class="hcm-fact"><small>Position</small>HR Operations Lead</div><div class="hcm-fact"><small>Requisition</small>REQ-301</div></div><div><h3>Proposed time</h3><div class="hcm-fact"><small>September 10, 2026</small>2:00 PM Eastern</div><div class="hcm-fact"><small>Candidate time</small>1:00 PM Central</div></div></div><p class="hcm-small">Draft schedule. No invitation has been sent.</p>';
    content +=
      '<div class="hcm-detail-footer"><button class="hcm-button" data-detail-back><i data-lucide="arrow-left" aria-hidden="true"></i>Back to work</button><span class="hcm-small">Changes remain local to this design preview.</span></div>';
    $("hcm-detail-content").innerHTML = content;
    icons();
    $("hcm-back").focus();
  }

  async function openDetail(id, button) {
    await simulate(() => renderDetail(id), {
      button,
      label: "Opening request",
      route: true,
    });
  }

  function personModal(id) {
    const person = people.find((entry) => entry.id === id);
    if (!person) return;
    openModal(
      "profile",
      '<div class="hcm-detail-heading" style="margin-top:0"><span class="hcm-avatar">' +
        person.initials +
        "</span><div><h2>" +
        person.name +
        '</h2><p class="hcm-small">' +
        person.role +
        " · " +
        person.team +
        '</p></div></div><div class="hcm-comparison"><div><h3>Work</h3><div class="hcm-fact"><small>Location</small>' +
        person.location +
        '</div><div class="hcm-fact"><small>Manager</small>' +
        person.manager +
        '</div></div><div><h3>Quick action</h3><p class="hcm-small" style="margin-top:10px">Open the fictional worker profile or begin a governed update.</p></div></div>',
      '<button class="hcm-button" data-modal-action="close">Close</button><button class="hcm-button hcm-primary" data-modal-action="profile-open">Open full profile</button>',
      person.name,
    );
  }

  function openModal(type, body, actions, title) {
    state.modal = type;
    const layer = $("hcm-modal-layer");
    if (layer.hidden) state.modalTrigger = document.activeElement;
    layer.innerHTML =
      '<section class="hcm-modal" role="dialog" aria-modal="true" aria-labelledby="hcm-modal-title"><header class="hcm-modal-head"><div><div class="hcm-eyebrow">' +
      (type === "decision"
        ? "Decision preview · No data submitted"
        : "Local interaction preview") +
      '</div><h2 id="hcm-modal-title">' +
      esc(title) +
      '</h2></div><button class="hcm-quiet" data-modal-action="close" aria-label="Close dialog"><i data-lucide="x" aria-hidden="true"></i></button></header><div class="hcm-modal-body">' +
      body +
      '</div><footer class="hcm-modal-actions">' +
      actions +
      "</footer></section>";
    layer.hidden = false;
    icons();
    layer.querySelector("button, input, select, textarea")?.focus();
  }

  function closeModal() {
    $("hcm-modal-layer").hidden = true;
    $("hcm-modal-layer").innerHTML = "";
    state.modal = null;
    if (state.modalTrigger?.isConnected) state.modalTrigger.focus();
    state.modalTrigger = null;
  }

  function openGuide() {
    openModal(
      "guide",
      '<p>Review what changes this year, compare plans, and prepare dependents before enrollment opens.</p><div class="hcm-evidence"><li><i data-lucide="calendar-check" aria-hidden="true"></i>Enrollment: Sep 21 – Oct 2</li><li><i data-lucide="file-check-2" aria-hidden="true"></i>Plan comparison ready</li><li><i data-lucide="shield-check" aria-hidden="true"></i>No elections are submitted in this demo</li></div>',
      '<button class="hcm-button" data-modal-action="close">Close</button><button class="hcm-button hcm-primary" data-modal-action="guide-open">Open guide</button>',
      "Benefits enrollment guide",
    );
  }

  function openHeadcount() {
    state.modalStep = 1;
    openModal(
      "headcount",
      '<div class="hcm-form-grid"><label class="hcm-field hcm-field-wide">Role title<input id="hcm-role-title" value="People Operations Manager"></label><label class="hcm-field">Team<select><option>People Operations</option><option>Strategy</option><option>Product</option></select></label><label class="hcm-field">Location<select><option>Chicago, IL</option><option>New York, NY</option><option>Remote</option></select></label><label class="hcm-field hcm-field-wide">Business reason<textarea id="hcm-business-reason" rows="3">Expand employee service coverage for North America.</textarea></label></div>',
      '<button class="hcm-button" data-modal-action="close">Cancel</button><button class="hcm-button hcm-primary" data-modal-action="headcount-review">Review request</button>',
      "Request headcount",
    );
  }

  function openSupport() {
    openModal(
      "support",
      '<div class="hcm-form-grid"><label class="hcm-field hcm-field-wide">Topic<select><option>Workflow help</option><option>Access and permissions</option><option>Technical issue</option></select></label><label class="hcm-field hcm-field-wide">What do you need help with?<textarea id="hcm-support-message" rows="4">I need help reviewing a promotion proposal.</textarea></label></div><p class="hcm-small" style="margin-top:14px">Demo only. This message stays in the browser.</p>',
      '<button class="hcm-button" data-modal-action="close">Cancel</button><button class="hcm-button hcm-primary" data-modal-action="support-send">Send demo request</button>',
      "Contact support",
    );
  }

  function openDecision() {
    const item = work.find((entry) => entry.id === state.lastWork);
    if (!item) return;
    const decisionBody =
      "<p>Choose an outcome for <strong>" +
      item.title +
      '</strong>. You can review the impact before the local simulation updates the queue.</p><div class="hcm-choice-row"><button class="hcm-choice" data-decision="approved" aria-pressed="true"><strong>Approve</strong><span class="hcm-small">Move to compensation review</span></button><button class="hcm-choice" data-decision="returned" aria-pressed="false"><strong>Return</strong><span class="hcm-small">Send back to Alex Morgan</span></button><button class="hcm-choice hcm-choice-danger" data-decision="rejected" aria-pressed="false"><strong>Reject</strong><span class="hcm-small">End this promotion request</span></button></div><div class="hcm-decision-impact" id="hcm-decision-impact"><i data-lucide="route" aria-hidden="true"></i><div><strong>Next: compensation review</strong><span>Approval advances the request; it does not update the worker record.</span></div></div><label class="hcm-field" style="margin-top:18px">Decision comment<textarea id="hcm-decision-comment" rows="3">Reviewed against role, range, and budget evidence.</textarea></label>';
    openModal(
      "decision",
      decisionBody,
      '<button class="hcm-button" data-modal-action="close">Cancel</button><button class="hcm-button hcm-primary" data-modal-action="decision-submit">Approve in preview</button>',
      "Review decision",
    );
    state.decision = "approved";
  }

  function toggleNotifications() {
    const panel = $("hcm-notification-panel");
    if (!panel.hidden) {
      panel.hidden = true;
      $("hcm-notifications").setAttribute("aria-expanded", "false");
      return;
    }
    panel.innerHTML =
      '<div class="hcm-popover-head"><div><h2>Notifications</h2><span class="hcm-small">' +
      (state.notificationsRead ? "All caught up" : "3 unread") +
      '</span></div><button class="hcm-quiet" data-notification-action="mark-read"' +
      (state.notificationsRead ? " disabled" : "") +
      '>Mark all read</button></div><button class="hcm-notice" data-notification-work="promotion"><strong>Promotion ready for review</strong><span class="hcm-small">Jordan Lee · due today</span></button><button class="hcm-notice" data-notification-work="coverage"><strong>Coverage decision requested</strong><span class="hcm-small">Avery Patel · begins Sep 8</span></button><button class="hcm-notice" data-notification-work="recruiting"><strong>Interview panel responded</strong><span class="hcm-small">Sam Rivera · 2:00 PM Eastern</span></button>';
    panel.hidden = false;
    $("hcm-notifications").setAttribute("aria-expanded", "true");
    icons();
  }

  function customize(open) {
    $("hcm-customizer").hidden = !open;
    $("hcm-customize").setAttribute("aria-expanded", String(open));
    $("hcm-customize").innerHTML =
      '<i data-lucide="sliders-horizontal" aria-hidden="true"></i>' +
      (open ? "Close customization" : "Customize workspace");
    icons();
  }

  function backToWork() {
    renderPage(state.page === "home" ? "home" : "work");
    root.querySelector('[data-work="' + state.lastWork + '"]')?.focus();
  }

  async function handleModalAction(action, button) {
    if (action === "close") {
      closeModal();
    } else if (action === "guide-open") {
      await simulate(
        () => {
          $("hcm-modal-layer").querySelector(".hcm-modal-body").innerHTML =
            '<div class="hcm-success-panel"><span class="hcm-success-icon"><i data-lucide="book-open-check" aria-hidden="true"></i></span><h2>Guide loaded</h2><p class="hcm-small" style="margin-top:8px">Plan comparison, enrollment dates, and preparation checklist are ready.</p></div>';
          button.textContent = "Guide open";
          icons();
        },
        { button, label: "Loading guide" },
      );
    } else if (action === "headcount-review") {
      const role = $("hcm-role-title").value.trim() || "Untitled role";
      $("hcm-modal-layer").querySelector(".hcm-modal-body").innerHTML =
        '<div class="hcm-comparison"><div><h3>Request</h3><div class="hcm-fact"><small>Role</small>' +
        esc(role) +
        '</div><div class="hcm-fact"><small>Team</small>People Operations</div></div><div><h3>Review</h3><div class="hcm-fact"><small>Hiring manager</small>Maya Chen</div><div class="hcm-fact"><small>Status after submission</small>Draft proposal</div></div></div><p class="hcm-small">No requisition or financial commitment is created in this demo.</p>';
      $("hcm-modal-layer").querySelector(".hcm-modal-actions").innerHTML =
        '<button class="hcm-button" data-modal-action="headcount-edit">Back</button><button class="hcm-button hcm-primary" data-modal-action="headcount-submit">Submit simulation</button>';
      icons();
    } else if (action === "headcount-edit") {
      openHeadcount();
    } else if (action === "headcount-submit") {
      await simulate(
        () => {
          closeModal();
          toast(
            "Demo request created",
            "HC-204 is a local preview; nothing was submitted.",
          );
        },
        { button, label: "Submitting" },
      );
    } else if (action === "support-send") {
      await simulate(
        () => {
          closeModal();
          toast(
            "Support request simulated",
            "A production request would show its case ID and delivery receipt.",
          );
        },
        { button, label: "Sending" },
      );
    } else if (action === "decision-submit") {
      const item = work.find((entry) => entry.id === state.lastWork);
      await simulate(
        () => {
          const labels = {
            approved: ["Approved locally", "hcm-success", "complete"],
            returned: ["Returned for changes", "hcm-wait", "waiting"],
            rejected: ["Rejected locally", "hcm-error", "complete"],
          };
          [item.status, item.tone, item.kind] = labels[state.decision];
          item.due = "Updated just now";
          closeModal();
          renderNav();
          renderDetail(item.id);
          toast(
            item.status,
            "Local demo state updated after simulated network latency.",
          );
        },
        { button, label: "Saving decision" },
      );
    } else if (action === "profile-open") {
      await simulate(
        () => {
          closeModal();
          renderPage("people");
          toast(
            "Profile opened",
            "The directory is filtered by your authorized scope.",
          );
        },
        { button, label: "Opening profile" },
      );
    }
  }

  async function handleRouteAction(action, button) {
    if (action === "org-filter") {
      await simulate(
        () => {
          state.orgFilter = !state.orgFilter;
          renderRoute("organization");
          toast(
            state.orgFilter ? "Team filter applied" : "Team filter cleared",
            state.orgFilter
              ? "Showing the Strategy reporting branch."
              : "Showing all authorized reporting branches.",
          );
        },
        { button, label: "Applying" },
      );
    } else if (action === "org-date" || action === "org-view") {
      await simulate(
        () =>
          toast(
            action === "org-date"
              ? "Historical view ready"
              : "Organization view updated",
            action === "org-date"
              ? "The chart remains anchored to the September 5 fixture."
              : "This slice keeps the chart view while preserving your controls.",
          ),
        { button, label: "Updating", delay: Math.min(state.latency, 500) },
      );
    } else if (action === "org-changes") {
      await simulate(
        () =>
          openModal(
            "org-changes",
            '<div class="hcm-scope-dialog"><span class="hcm-status hcm-success">1 upcoming</span><h3 style="margin-top:14px">Jordan Lee · Promotion proposal</h3><p class="hcm-small">Proposed effective date Sep 15, 2026. This proposal has not changed the reporting structure.</p></div>',
            '<button class="hcm-button hcm-primary" data-modal-action="close">Done</button>',
            "Upcoming organization changes",
          ),
        { button, label: "Loading changes" },
      );
    } else if (action === "people-filter") {
      await simulate(
        () => {
          state.peopleFilter = state.peopleFilter === "all" ? "Product" : "all";
          renderPeople();
          toast(
            state.peopleFilter === "all" ? "Team filter cleared" : "Team filtered",
            state.peopleFilter === "all"
              ? "Showing all people in your authorized scope."
              : "Showing the Product team in your authorized scope.",
          );
        },
        { button, label: "Applying", delay: Math.min(state.latency, 600) },
      );
    } else if (action === "export-report") {
      await simulate(
        () =>
          toast(
            "Export preview ready",
            "The demo prepared a fictional CSV receipt; no file was downloaded.",
          ),
        { button, label: "Preparing" },
      );
    } else if (action === "report-filter") {
      await simulate(
        () =>
          toast(
            "Report filter applied",
            "Charts and certified reports now reflect the selected view.",
          ),
        { button, label: "Applying filter", delay: Math.min(state.latency, 550) },
      );
    } else if (action === "browse-reports" || action === "report-open") {
      await simulate(
        () =>
          openModal(
            "reports",
            '<div class="hcm-scope-dialog"><span class="hcm-status hcm-success">Certified</span><h3 style="margin-top:14px">Workforce movement</h3><p class="hcm-small">Hires, transfers, promotions, and exits within your authorized North America scope.</p><div class="hcm-definition"><strong>Definition:</strong> governed changes grouped by their committed effective date.</div></div>',
            '<button class="hcm-button" data-route-action="export-report">Export data</button><button class="hcm-button hcm-primary" data-modal-action="close">Done</button>',
            "Certified reports",
          ),
        { button, label: "Loading report" },
      );
    } else if (action === "review-transactions") {
      await navigate("work", button);
      toast("Transaction view loaded", "Use a request row to preview its details.");
    } else if (action === "contact-support") {
      openSupport();
    } else if (action === "help-promotion") {
      await openDetail("promotion", button);
    } else if (action === "help-customize" || action === "open-customizer") {
      customize(true);
      await navigate("home", button);
      customize(true);
    } else if (action === "help-permissions") {
      openModal(
        "permissions",
        '<p>Audience controls who receives a page. Authorization still controls every record, field, and action at runtime.</p><ul class="hcm-evidence"><li><i data-lucide="shield-check" aria-hidden="true"></i>Reauthorize deep links</li><li><i data-lucide="eye-off" aria-hidden="true"></i>Hide inaccessible fields and counts</li><li><i data-lucide="history" aria-hidden="true"></i>Record governed decisions</li></ul>',
        '<button class="hcm-button hcm-primary" data-modal-action="close">Got it</button>',
        "Access and scope",
      );
    } else if (action === "save-preferences") {
      await simulate(
        () =>
          toast(
            "Preferences saved locally",
            "Density and language choices are simulated in this browser.",
          ),
        { button, label: "Saving" },
      );
    } else if (action === "settings-section" || action === "theme-choice") {
      await simulate(
        () =>
          toast(
            "Preference preview updated",
            "This design slice keeps you on the preferences screen.",
          ),
        { button, label: "Loading", delay: Math.min(state.latency, 450) },
      );
    } else if (action === "activity-open") {
      await simulate(
        () =>
          openModal(
            "activity",
            '<p>The governed change completed successfully and its receipt is available to authorized reviewers.</p><ul class="hcm-evidence"><li><i data-lucide="circle-check"></i>Policy checks recorded</li><li><i data-lucide="history"></i>Decision and actor attributed</li><li><i data-lucide="lock-keyhole"></i>Private fields remain protected</li></ul>',
            '<button class="hcm-button hcm-primary" data-modal-action="close">Done</button>',
            "Completion receipt",
          ),
        { button, label: "Loading receipt" },
      );
    } else if (action.startsWith("admin-")) {
      const labels = {
        "admin-experience": "Experience Studio",
        "admin-workflows": "Workflow catalog",
        "admin-policies": "Access policy review",
        "admin-integrations": "Integration health",
        "admin-audit": "Configuration audit",
      };
      await simulate(
        () =>
          openModal(
            "admin-preview",
            '<div class="hcm-scope-dialog"><span class="hcm-status hcm-success">Preview ready</span><h3 style="margin-top:14px">' +
              labels[action] +
              '</h3><p class="hcm-small">This customer-governed surface is represented in the production design corpus. Changes in this mockup remain local.</p><div class="hcm-access-note"><i data-lucide="shield-check"></i><p>Publishing requires an authorized admin and creates an audit receipt.</p></div></div>',
            '<button class="hcm-button hcm-primary" data-modal-action="close">Done</button>',
            labels[action],
          ),
        { button, label: "Opening" },
      );
    }
  }

  $("hcm-customize").addEventListener("click", () =>
    customize($("hcm-customizer").hidden),
  );
  $("hcm-menu").addEventListener("click", () => {
    const open = root.dataset.menu !== "open";
    root.dataset.menu = open ? "open" : "closed";
    $("hcm-menu").setAttribute("aria-expanded", String(open));
  });
  $("hcm-notifications").setAttribute("aria-expanded", "false");
  $("hcm-notifications").setAttribute("aria-controls", "hcm-notification-panel");
  $("hcm-notifications").addEventListener("click", toggleNotifications);
  $("hcm-scope").addEventListener("click", () =>
    openModal(
      "scope",
      '<div class="hcm-scope-dialog"><span class="hcm-small">CURRENT SCOPE</span><button class="hcm-choice" data-modal-action="close" aria-pressed="true"><strong>People Operations · North America</strong><span class="hcm-small">Authorized people, work, and insights</span></button><button class="hcm-choice" data-modal-action="close" aria-pressed="false"><strong>My team</strong><span class="hcm-small">Direct and indirect reporting context</span></button><div class="hcm-access-note"><i data-lucide="shield-check"></i><p>Changing scope never expands your underlying permissions.</p></div></div>',
      '<button class="hcm-button hcm-primary" data-modal-action="close">Keep current scope</button>',
      "Choose scope",
    ),
  );
  $("hcm-open-work").addEventListener("click", (event) =>
    navigate("work", event.currentTarget),
  );
  $("hcm-help").addEventListener("click", (event) =>
    navigate("help", event.currentTarget),
  );
  $("hcm-settings").addEventListener("click", (event) =>
    navigate("settings", event.currentTarget),
  );
  $("hcm-guide").addEventListener("click", openGuide);
  $("hcm-back").addEventListener("click", backToWork);

  for (const [id, key] of [
    ["hcm-shape", "shape"],
    ["hcm-layout", "layout"],
    ["hcm-density", "density"],
    ["hcm-navigation", "navigation"],
    ["hcm-nav-tone", "navTone"],
  ]) {
    $(id).addEventListener("change", (event) => {
      state[key] = event.target.value;
      applyTheme();
      if (state.page === "settings") renderRoute("settings");
    });
  }
  $("hcm-latency").addEventListener("change", (event) => {
    state.latency = Number(event.target.value);
    toast(
      "Network simulation updated",
      event.target.options[event.target.selectedIndex].text +
        " will apply to data actions.",
    );
  });
  $("hcm-company-input").addEventListener("input", (event) => {
    state.company = event.target.value;
    applyTheme();
  });
  $("hcm-accent-input").addEventListener("input", (event) => {
    state.accent = event.target.value;
    applyTheme();
  });

  $("hcm-search-input").addEventListener("input", (event) => {
    state.search = event.target.value.trim().toLowerCase();
    clearTimeout(state.searchTimer);
    if (state.page === "home" || state.page === "work")
      $("hcm-work-list").innerHTML =
        '<div class="hcm-empty"><span class="hcm-spinner" style="display:inline-block" aria-hidden="true"></span><div>Searching authorized work…</div></div>';
    if (state.page === "people")
      $("hcm-directory").innerHTML = loadingMarkup("Searching people");
    state.searchTimer = setTimeout(
      () => {
        if (state.page === "home" || state.page === "work") renderWork();
        else if (state.page === "people") renderPeople();
        else if (state.search.length >= 2) renderPage("people");
      },
      Math.min(state.latency, 650),
    );
  });

  $("hcm-reset").addEventListener("click", (event) =>
    simulate(
      () => {
        Object.assign(state, initial);
        $("hcm-nav-tone").value = initial.navTone;
        $("hcm-latency").value = String(initial.latency);
        $("hcm-company-input").value = initial.company;
        $("hcm-accent-input").value = initial.accent;
        for (const [id, key] of [
          ["hcm-shape", "shape"],
          ["hcm-layout", "layout"],
          ["hcm-density", "density"],
          ["hcm-navigation", "navigation"],
        ])
          $(id).value = initial[key];
        applyTheme();
        toast(
          "Defaults restored",
          "Brand, layout, density, and navigation returned to HCM Next defaults.",
        );
      },
      { button: event.currentTarget, label: "Restoring", delay: 500 },
    ),
  );

  root.addEventListener("click", async (event) => {
    const button = event.target.closest("button");
    if (!button || !root.contains(button)) return;

    if (button.dataset.accent) {
      state.accent = button.dataset.accent;
      $("hcm-accent-input").value = state.accent;
      applyTheme();
    }
    if (button.dataset.page) await navigate(button.dataset.page, button);
    if (button.dataset.filter) {
      if (state.busy) return;
      const filter = button.dataset.filter;
      $("hcm-work-list").innerHTML =
        '<div class="hcm-empty"><span class="hcm-spinner" style="display:inline-block" aria-hidden="true"></span><div>Refreshing work…</div></div>';
      await simulate(
        () => {
          state.filter = filter;
          renderWork();
        },
        { button, label: "Refreshing", delay: Math.min(state.latency, 600) },
      );
    }
    if (button.dataset.work) {
      if (state.page === "work") {
        await simulate(
          () => {
            state.selectedWork = button.dataset.work;
            renderWork();
            renderWorkPreview();
          },
          { button, label: "Loading preview", delay: Math.min(state.latency, 550) },
        );
      } else await openDetail(button.dataset.work, button);
    }
    if (button.dataset.openWork) await openDetail(button.dataset.openWork, button);
    if (button.dataset.personSelect) {
      await simulate(
        () => {
          state.selectedPerson = button.dataset.personSelect;
          renderPeople();
        },
        { button, label: "Loading profile", delay: Math.min(state.latency, 550) },
      );
    }
    if (button.dataset.personModal) {
      await simulate(() => personModal(button.dataset.personModal), {
        button,
        label: "Opening profile",
        delay: Math.min(state.latency, 700),
      });
    }
    if (button.dataset.person) {
      await simulate(() => personModal(button.dataset.person), {
        button,
        label: "Opening profile",
        delay: Math.min(state.latency, 700),
      });
    }
    if (button.hasAttribute("data-detail-back")) backToWork();
    if (button.hasAttribute("data-preview-decision")) openDecision();
    if (button.dataset.action === "promotion") await openDetail("promotion", button);
    if (button.dataset.action === "hire") openHeadcount();
    if (button.dataset.action === "profile") await navigate("people", button);
    if (button.dataset.routeAction)
      await handleRouteAction(button.dataset.routeAction, button);
    if (button.dataset.modalAction)
      await handleModalAction(button.dataset.modalAction, button);
    if (button.dataset.decision) {
      state.decision = button.dataset.decision;
      $("hcm-modal-layer")
        .querySelectorAll("[data-decision]")
        .forEach((choice) =>
          choice.setAttribute("aria-pressed", String(choice === button)),
        );
      const decisionCopy = {
        approved: [
          "Next: compensation review",
          "Approval advances the request; it does not update the worker record.",
          "Approve in preview",
        ],
        returned: [
          "Next: revision by Alex Morgan",
          "The requester receives your comment and can submit a new revision.",
          "Return in preview",
        ],
        rejected: [
          "Next: request closed",
          "The promotion path ends and the worker record remains unchanged.",
          "Reject in preview",
        ],
      }[state.decision];
      $("hcm-decision-impact").querySelector("strong").textContent = decisionCopy[0];
      $("hcm-decision-impact").querySelector("span").textContent = decisionCopy[1];
      $("hcm-modal-layer").querySelector(
        '[data-modal-action="decision-submit"]',
      ).textContent = decisionCopy[2];
    }
    if (button.dataset.settingChoice) {
      state.density = button.dataset.settingChoice;
      $("hcm-density").value = state.density;
      applyTheme();
      renderRoute("settings");
    }
    if (button.dataset.notificationAction === "mark-read") {
      await simulate(
        () => {
          state.notificationsRead = true;
          root.dataset.notifications = "read";
          $("hcm-notifications").setAttribute(
            "aria-label",
            "Notifications, no unread items",
          );
          $("hcm-notification-panel").querySelector(".hcm-small").textContent =
            "All caught up";
          toast("Notifications marked as read");
        },
        { button, label: "Updating", delay: Math.min(state.latency, 600) },
      );
      button.disabled = true;
    }
    if (button.dataset.notificationWork) {
      $("hcm-notification-panel").hidden = true;
      $("hcm-notifications").setAttribute("aria-expanded", "false");
      await openDetail(button.dataset.notificationWork, button);
    }
  });

  $("hcm-modal-layer").addEventListener("click", (event) => {
    if (event.target === $("hcm-modal-layer")) closeModal();
  });
  document.addEventListener("keydown", (event) => {
    if (event.key === "Tab" && !$("hcm-modal-layer").hidden) {
      const focusable = Array.from(
        $("hcm-modal-layer").querySelectorAll(
          'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [href], [tabindex]:not([tabindex="-1"])',
        ),
      );
      const first = focusable[0];
      const last = focusable.at(-1);
      if (!first) return;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }
    if (event.key === "Escape") {
      if (!$("hcm-modal-layer").hidden) closeModal();
      else if (!$("hcm-notification-panel").hidden) {
        $("hcm-notification-panel").hidden = true;
        $("hcm-notifications").setAttribute("aria-expanded", "false");
        $("hcm-notifications").focus();
      }
    }
  });

  root.dataset.notifications = "unread";
  applyTheme();
  renderPage("home");
})();

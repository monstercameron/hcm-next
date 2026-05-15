import {
  CheckCircle2,
  ClipboardList,
  FileClock,
  FilePenLine,
  GitBranch,
  LayoutDashboard,
  PanelTop,
  Settings2,
  SlidersHorizontal,
  SquareStack,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

export type PageRoute = {
  path: string;
  pageId: string;
  label: string;
  description: string;
  icon: LucideIcon;
};

export type NavSection = {
  label: string;
  routes: readonly PageRoute[];
};

export const navSections: readonly NavSection[] = [
  {
    label: "Workflow Pages",
    routes: [
      {
        path: "/",
        pageId: "change-request-hub",
        label: "Hub",
        description: "Requests, tasks, and repair work",
        icon: LayoutDashboard,
      },
      {
        path: "/request",
        pageId: "manager-request",
        label: "Request",
        description: "Generated manager intake",
        icon: FilePenLine,
      },
      {
        path: "/approval",
        pageId: "approval-review",
        label: "Approval",
        description: "Decision and risk context",
        icon: CheckCircle2,
      },
      {
        path: "/simulation",
        pageId: "simulation",
        label: "Simulation",
        description: "Pre-execution impact",
        icon: GitBranch,
      },
      {
        path: "/timeline",
        pageId: "audit-timeline",
        label: "Timeline",
        description: "Ledger-derived audit trail",
        icon: FileClock,
      },
      {
        path: "/admin-preview",
        pageId: "admin-preview",
        label: "Preview",
        description: "Actor and surface preview",
        icon: Settings2,
      },
    ],
  },
  {
    label: "Control Types",
    routes: [
      {
        path: "/controls/basic-inputs",
        pageId: "controls-basic-inputs",
        label: "Basic Inputs",
        description: "Text, number, date, time, contact",
        icon: SlidersHorizontal,
      },
      {
        path: "/controls/choice-selection",
        pageId: "controls-choice-selection",
        label: "Choice Selectors",
        description: "Dropdowns, radios, filters",
        icon: SlidersHorizontal,
      },
      {
        path: "/controls/toggle-slider",
        pageId: "controls-toggle-slider",
        label: "Toggles + Sliders",
        description: "Settings, thresholds, scores",
        icon: SlidersHorizontal,
      },
      {
        path: "/controls/structured-groups",
        pageId: "controls-structured-groups",
        label: "Structured Groups",
        description: "Lists, tables, matrices, clusters",
        icon: SlidersHorizontal,
      },
      {
        path: "/controls/files-signatures",
        pageId: "controls-files-signatures",
        label: "Files + Signatures",
        description: "Evidence, attestations, signing",
        icon: SlidersHorizontal,
      },
    ],
  },
  {
    label: "HCM Controls",
    routes: [
      {
        path: "/controls/hcm-domain",
        pageId: "controls-hcm-domain",
        label: "HCM Domain",
        description: "Employee, org, pay, role editors",
        icon: SlidersHorizontal,
      },
      {
        path: "/controls/entity-access",
        pageId: "controls-entity-access",
        label: "Entity Access",
        description: "RBAC-scoped pickers and org trees",
        icon: Settings2,
      },
      {
        path: "/controls/effective-change",
        pageId: "controls-effective-change",
        label: "Effective Changes",
        description: "Dated changes and field diffs",
        icon: FileClock,
      },
      {
        path: "/controls/comp-schedule",
        pageId: "controls-comp-schedule",
        label: "Comp + Schedule",
        description: "Pay package, FTE, shifts, time zones",
        icon: SlidersHorizontal,
      },
    ],
  },
  {
    label: "Governance Controls",
    routes: [
      {
        path: "/controls/approval-policy-ai",
        pageId: "controls-approval-policy-ai",
        label: "Approvals + AI",
        description: "Chains, evidence, AI review",
        icon: CheckCircle2,
      },
      {
        path: "/controls/bulk-repair",
        pageId: "controls-bulk-repair",
        label: "Bulk + Repair",
        description: "Bulk grids, conflicts, integration repair",
        icon: GitBranch,
      },
      {
        path: "/controls/privacy-regional-simulation",
        pageId: "controls-privacy-regional-simulation",
        label: "Privacy + Simulation",
        description: "Sensitive reveal, regional data, plans",
        icon: Settings2,
      },
    ],
  },
  {
    label: "Item Types",
    routes: [
      {
        path: "/items/content",
        pageId: "items-content",
        label: "Content Items",
        description: "Text, markdown, HTML, FAQ, links",
        icon: SquareStack,
      },
      {
        path: "/items/data-display",
        pageId: "items-data-display",
        label: "Data Displays",
        description: "Tables, graphs, metrics, clusters",
        icon: SquareStack,
      },
      {
        path: "/items/workflow-governed",
        pageId: "items-workflow-governed",
        label: "Workflow Items",
        description: "Queues, diffs, approvals, audit",
        icon: SquareStack,
      },
      {
        path: "/items/media",
        pageId: "items-media",
        label: "Media Items",
        description: "Image, audio, video, PDF",
        icon: SquareStack,
      },
    ],
  },
  {
    label: "HCM Widgets",
    routes: [
      {
        path: "/widgets/hcm-insights",
        pageId: "widgets-hcm-insights",
        label: "HCM Insights",
        description: "Comp, payroll, benefits, compliance",
        icon: SquareStack,
      },
      {
        path: "/widgets/workflow-ops",
        pageId: "widgets-workflow-ops",
        label: "Workflow Ops",
        description: "SLA, routing, repair, policy",
        icon: GitBranch,
      },
    ],
  },
  {
    label: "HR Lifecycle Widgets",
    routes: [
      {
        path: "/widgets/recruiting",
        pageId: "widgets-recruiting",
        label: "Recruiting",
        description: "Pipeline, interviews, offers",
        icon: ClipboardList,
      },
      {
        path: "/widgets/onboarding",
        pageId: "widgets-onboarding",
        label: "Onboarding",
        description: "New hire tasks, access, docs",
        icon: FilePenLine,
      },
      {
        path: "/widgets/offboarding",
        pageId: "widgets-offboarding",
        label: "Offboarding",
        description: "Exit, assets, access, final pay",
        icon: FileClock,
      },
    ],
  },
  {
    label: "Talent Widgets",
    routes: [
      {
        path: "/widgets/performance",
        pageId: "widgets-performance",
        label: "Performance",
        description: "Goals, reviews, calibration",
        icon: SquareStack,
      },
      {
        path: "/widgets/talent",
        pageId: "widgets-talent",
        label: "Talent",
        description: "9-box, skills, succession",
        icon: PanelTop,
      },
      {
        path: "/widgets/workforce-planning",
        pageId: "widgets-workforce-planning",
        label: "Workforce Planning",
        description: "Headcount, positions, forecast",
        icon: GitBranch,
      },
    ],
  },
  {
    label: "Workforce Ops Widgets",
    routes: [
      {
        path: "/widgets/scheduling-attendance",
        pageId: "widgets-scheduling-attendance",
        label: "Scheduling",
        description: "Shifts, timecards, coverage",
        icon: FileClock,
      },
      {
        path: "/widgets/leave-absence",
        pageId: "widgets-leave-absence",
        label: "Leave",
        description: "FMLA, accrual, conflicts",
        icon: FilePenLine,
      },
      {
        path: "/widgets/benefits",
        pageId: "widgets-benefits",
        label: "Benefits",
        description: "Enrollment, costs, COBRA",
        icon: SquareStack,
      },
      {
        path: "/widgets/payroll-tax",
        pageId: "widgets-payroll-tax",
        label: "Payroll + Tax",
        description: "Gross-net, taxes, exceptions",
        icon: ClipboardList,
      },
    ],
  },
  {
    label: "HR Risk + Experience",
    routes: [
      {
        path: "/widgets/employee-relations",
        pageId: "widgets-employee-relations",
        label: "Employee Relations",
        description: "Cases, investigations, notes",
        icon: Settings2,
      },
      {
        path: "/widgets/compliance-risk",
        pageId: "widgets-compliance-risk",
        label: "Compliance",
        description: "Attestations, deadlines, audit",
        icon: CheckCircle2,
      },
      {
        path: "/widgets/employee-experience",
        pageId: "widgets-employee-experience",
        label: "Experience",
        description: "Pulse, eNPS, recognition",
        icon: PanelTop,
      },
    ],
  },
  {
    label: "HR Core + Growth",
    routes: [
      {
        path: "/widgets/core-hris",
        pageId: "widgets-core-hris",
        label: "Core HRIS",
        description: "Employee 360, history, roster",
        icon: LayoutDashboard,
      },
      {
        path: "/widgets/compensation-advanced",
        pageId: "widgets-compensation-advanced",
        label: "Advanced Comp",
        description: "Rewards, merit, equity",
        icon: SquareStack,
      },
      {
        path: "/widgets/learning-development",
        pageId: "widgets-learning-development",
        label: "Learning",
        description: "Plans, skills, courses, CEUs",
        icon: ClipboardList,
      },
    ],
  },
  {
    label: "Equity Labor + Safety",
    routes: [
      {
        path: "/widgets/dei-equity",
        pageId: "widgets-dei-equity",
        label: "DEI + Equity",
        description: "Representation, impact, equity",
        icon: PanelTop,
      },
      {
        path: "/widgets/labor-union",
        pageId: "widgets-labor-union",
        label: "Labor + Union",
        description: "Seniority, bids, grievances",
        icon: GitBranch,
      },
      {
        path: "/widgets/health-safety",
        pageId: "widgets-health-safety",
        label: "Health + Safety",
        description: "Incidents, OSHA, comp cases",
        icon: CheckCircle2,
      },
    ],
  },
  {
    label: "HR Services + Global",
    routes: [
      {
        path: "/widgets/hr-service-delivery",
        pageId: "widgets-hr-service-delivery",
        label: "Service Delivery",
        description: "Cases, KB, SLA, CSAT",
        icon: Settings2,
      },
      {
        path: "/widgets/employee-finance",
        pageId: "widgets-employee-finance",
        label: "Employee Finance",
        description: "Expenses, tuition, advances",
        icon: FilePenLine,
      },
      {
        path: "/widgets/global-mobility",
        pageId: "widgets-global-mobility",
        label: "Global Mobility",
        description: "Visa, immigration, relocation",
        icon: FileClock,
      },
      {
        path: "/widgets/privacy-security",
        pageId: "widgets-privacy-security",
        label: "Privacy",
        description: "DSR, access, legal hold",
        icon: CheckCircle2,
      },
    ],
  },
  {
    label: "Analysis Widgets",
    routes: [
      {
        path: "/widgets/data-viz",
        pageId: "widgets-data-viz",
        label: "Data Viz",
        description: "Bar, pie, scatter, heatmap",
        icon: SquareStack,
      },
      {
        path: "/widgets/time",
        pageId: "widgets-time",
        label: "Time",
        description: "Clocks, countdowns, timers",
        icon: FileClock,
      },
      {
        path: "/widgets/ai-governance",
        pageId: "widgets-ai-governance",
        label: "AI Governance",
        description: "Risk, citations, audit, fairness",
        icon: Settings2,
      },
    ],
  },
  {
    label: "Work Surface Widgets",
    routes: [
      {
        path: "/widgets/documents-evidence",
        pageId: "widgets-documents-evidence",
        label: "Docs + Evidence",
        description: "Packets, OCR, redaction, export",
        icon: FilePenLine,
      },
      {
        path: "/widgets/collaboration",
        pageId: "widgets-collaboration",
        label: "Collaboration",
        description: "Comments, notes, assignments",
        icon: ClipboardList,
      },
      {
        path: "/widgets/integration",
        pageId: "widgets-integration",
        label: "Integration",
        description: "Sync, webhooks, payloads, repair",
        icon: GitBranch,
      },
      {
        path: "/widgets/ui-blocks",
        pageId: "widgets-ui-blocks",
        label: "UI Blocks",
        description: "Tabs, kanban, calendar, states",
        icon: PanelTop,
      },
    ],
  },
  {
    label: "Compositions",
    routes: [
      {
        path: "/examples/professional",
        pageId: "example-professional-blend",
        label: "Professional Blend",
        description: "Mixed workflow page",
        icon: PanelTop,
      },
      {
        path: "/tasks",
        pageId: "change-request-hub",
        label: "Tasks",
        description: "Approval queue view",
        icon: ClipboardList,
      },
    ],
  },
];

export const pageRoutes: readonly PageRoute[] = navSections.flatMap(
  (section) => section.routes,
);

export const hiddenPageRoutes: readonly PageRoute[] = [
  {
    path: "/examples/controls",
    pageId: "example-all-controls",
    label: "All Controls",
    description: "Legacy all-controls catalog",
    icon: SlidersHorizontal,
  },
  {
    path: "/examples/widgets",
    pageId: "example-all-widgets",
    label: "All Widgets",
    description: "Legacy all-widgets catalog",
    icon: SquareStack,
  },
];

export const appRoutes: readonly PageRoute[] = [...pageRoutes, ...hiddenPageRoutes];

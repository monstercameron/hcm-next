import type { PageDefinition, WidgetInstance } from "@hcm-next/ui-contracts";

const basePageDefinitions: readonly PageDefinition[] = [
  {
    id: "change-request-hub",
    title: "Change Request Hub",
    description: "Operational queue for workflow requests, approvals, and repair work.",
    workflowTypes: ["*"],
    surfaceModes: ["full_app", "customer_portal", "admin_preview", "mobile_compact"],
    regions: [
      {
        id: "main",
        layout: "stack",
        width: "wide",
        widgets: [
          {
            id: "hub-intro",
            type: "content.callout",
            title: "Today",
            props: {
              tone: "info",
              body: "Workflow requests are routed by state, actor permissions, due date, and risk.",
            },
          },
          {
            id: "active-requests",
            type: "queue.requestList",
            title: "Active requests",
            props: {
              requests: [
                {
                  id: "wf-org-comp-1048",
                  title: "Org transfer and compensation change",
                  employee: "Jane Rivera",
                  state: "Waiting HRBP approval",
                  risk: "Medium",
                  due: "Today",
                },
                {
                  id: "wf-headcount-211",
                  title: "Senior Registered Nurse headcount",
                  employee: "Cambridge Nursing",
                  state: "Waiting finance review",
                  risk: "High",
                  due: "Tomorrow",
                },
                {
                  id: "wf-contact-882",
                  title: "Emergency contact update",
                  employee: "Mina Patel",
                  state: "Ready to execute",
                  risk: "Low",
                  due: "No SLA",
                },
              ],
            },
          },
          {
            id: "hub-markdown",
            type: "content.markdown",
            title: "Review posture",
            props: {
              markdown:
                "High-risk changes should show policy findings, downstream impact, AI visibility, and simulation state before approval.",
            },
          },
        ],
      },
    ],
  },
  {
    id: "manager-request",
    title: "Manager Request",
    description: "Manager-facing request page for a workflow-specific employee change.",
    workflowTypes: ["employee.org_transfer_compensation_change"],
    surfaceModes: [
      "full_app",
      "customer_portal",
      "embedded_manager_widget",
      "admin_preview",
      "mobile_compact",
    ],
    regions: [
      {
        id: "main",
        layout: "grid",
        width: "wide",
        widgets: [
          {
            id: "employee",
            type: "employee.summary",
            title: "Employee",
            bindings: {
              employee: {
                source: "employee_projection",
              },
            },
          },
          {
            id: "request-fields",
            type: "form.dynamicFieldGroup",
            title: "Requested change",
            props: {
              fields: [
                {
                  id: "newManager",
                  label: "New manager",
                  type: "employee_lookup",
                  required: true,
                },
                {
                  id: "newDepartment",
                  label: "New department",
                  type: "department_lookup",
                  required: true,
                },
                {
                  id: "effectiveDate",
                  label: "Effective date",
                  type: "date",
                  required: true,
                },
                {
                  id: "businessReason",
                  label: "Business reason",
                  type: "textarea",
                  required: true,
                },
              ],
            },
          },
          {
            id: "change-diff",
            type: "change.diff",
            title: "Current vs proposed",
            bindings: {
              current: {
                source: "workflow_context",
                path: "current",
              },
              proposed: {
                source: "workflow_input",
                path: "proposed",
              },
            },
          },
          {
            id: "manager-image",
            type: "media.image",
            title: "Team context",
            props: {
              src: "https://images.unsplash.com/photo-1552664730-d307ca884978?auto=format&fit=crop&w=1200&q=80",
              alt: "Team planning session",
            },
          },
        ],
      },
    ],
  },
  {
    id: "approval-review",
    title: "Approval Review",
    description:
      "Reviewer-facing page for decision, risk context, and permitted details.",
    workflowTypes: ["employee.org_transfer_compensation_change"],
    surfaceModes: [
      "full_app",
      "embedded_approval_widget",
      "hrbp_workbench",
      "compensation_review",
      "finance_review",
      "admin_preview",
      "mobile_compact",
    ],
    regions: [
      {
        id: "main",
        layout: "grid",
        width: "wide",
        widgets: [
          {
            id: "approval-employee",
            type: "employee.summary",
            title: "Subject",
            bindings: {
              employee: {
                source: "employee_projection",
              },
            },
          },
          {
            id: "approval-diff",
            type: "change.diff",
            title: "Change detail",
            bindings: {
              current: {
                source: "workflow_context",
                path: "current",
              },
              proposed: {
                source: "workflow_input",
                path: "proposed",
              },
            },
          },
          {
            id: "approval-actions",
            type: "approval.decisionPanel",
            title: "Decision",
            actions: [
              {
                action: "approve",
                label: "Approve",
                transition: "approve",
                variant: "primary",
              },
              {
                action: "reject",
                label: "Reject",
                transition: "reject",
                requiresReason: true,
                variant: "danger",
              },
              {
                action: "request_more_information",
                label: "Request info",
                transition: "request_more_information",
                variant: "secondary",
              },
            ],
          },
          {
            id: "ai-brief",
            type: "ai.changeBrief",
            title: "AI review",
            props: {
              summary:
                "The change is within the configured approval path. Compensation impact requires compensation review before execution.",
              visibility:
                "AI received proposed job, department, effective date, and permitted compensation band data.",
            },
          },
        ],
      },
    ],
  },
  {
    id: "simulation",
    title: "Simulation",
    description:
      "Pre-execution transaction plan, policy findings, and downstream impact.",
    workflowTypes: ["employee.org_transfer_compensation_change"],
    surfaceModes: [
      "full_app",
      "customer_portal",
      "hrbp_workbench",
      "compensation_review",
      "payroll_review",
      "finance_review",
      "admin_preview",
    ],
    regions: [
      {
        id: "main",
        layout: "stack",
        width: "wide",
        widgets: [
          {
            id: "simulation-results",
            type: "simulation.resultPanel",
            title: "Transaction simulation",
            props: {
              checks: [
                {
                  label: "Payroll cutoff",
                  status: "warning",
                  detail: "Effective date is inside the next payroll window.",
                },
                {
                  label: "Compensation band",
                  status: "success",
                  detail: "Proposed amount is within approved band.",
                },
                {
                  label: "Identity access",
                  status: "info",
                  detail: "Department change will update downstream access groups.",
                },
              ],
            },
          },
          {
            id: "simulation-html",
            type: "content.html",
            title: "Policy excerpt",
            props: {
              html: "<strong>Execution rule:</strong> approvals and simulation must both be complete before writes are released.",
            },
          },
        ],
      },
    ],
  },
  {
    id: "audit-timeline",
    title: "Audit Timeline",
    description: "Ledger-derived timeline for business, audit, and repair views.",
    workflowTypes: ["employee.org_transfer_compensation_change"],
    surfaceModes: ["full_app", "customer_portal", "admin_preview", "audit_export"],
    regions: [
      {
        id: "main",
        layout: "stack",
        width: "wide",
        widgets: [
          {
            id: "timeline",
            type: "audit.timeline",
            title: "Timeline",
            props: {
              events: [
                {
                  at: "2026-05-15T09:05:00Z",
                  actor: "Alex Manager",
                  label: "Submitted request",
                },
                {
                  at: "2026-05-15T10:20:00Z",
                  actor: "Riley HRBP",
                  label: "Approved HRBP review",
                },
                {
                  at: "2026-05-15T11:10:00Z",
                  actor: "System",
                  label: "Simulation completed",
                },
              ],
            },
          },
          {
            id: "timeline-audio",
            type: "media.audio",
            title: "Optional audio note",
            props: {
              src: "",
              transcript:
                "Audio player placeholder for customer-provided instructions or accessibility notes.",
            },
          },
        ],
      },
    ],
  },
  {
    id: "admin-preview",
    title: "Admin Preview",
    description: "Preview dynamic pages by actor, workflow state, surface, and brand.",
    workflowTypes: ["*"],
    surfaceModes: ["admin_preview", "full_app"],
    regions: [
      {
        id: "main",
        layout: "grid",
        width: "wide",
        widgets: [
          {
            id: "preview-copy",
            type: "content.text",
            title: "Preview controls",
            props: {
              body: "Use actor, state, surface, and brand previews before publishing workflow page definitions.",
            },
          },
          {
            id: "preview-video",
            type: "media.video",
            title: "Training video",
            props: {
              src: "",
              transcript: "Video placeholder for a customer-authored walkthrough.",
            },
          },
          {
            id: "preview-pdf",
            type: "media.pdf",
            title: "Policy PDF",
            props: {
              src: "",
              label: "Policy document preview placeholder",
            },
          },
          {
            id: "preview-links",
            type: "content.linkList",
            title: "Related admin checks",
            props: {
              links: [
                {
                  label: "Sensitive masking preview",
                  href: "#",
                },
                {
                  label: "Mobile layout preview",
                  href: "#",
                },
                {
                  label: "Audit export preview",
                  href: "#",
                },
              ],
            },
          },
        ],
      },
    ],
  },
  {
    id: "example-all-controls",
    title: "All Controls Example",
    description:
      "Generated form page showing the full control palette for workflow intake and review pages.",
    workflowTypes: ["*"],
    surfaceModes: ["full_app", "admin_preview", "mobile_compact"],
    regions: [
      {
        id: "core-controls",
        title: "Core controls",
        layout: "grid",
        width: "wide",
        widgets: [
          {
            id: "basic-form-controls",
            type: "form.dynamicFieldGroup",
            title: "Basic input controls",
            props: {
              fields: [
                {
                  id: "text",
                  label: "Text input",
                  type: "text",
                  required: true,
                  placeholder: "Senior Registered Nurse",
                  minLength: 4,
                  maxLength: 80,
                  help: "Required text with minimum and maximum length.",
                },
                {
                  id: "textarea",
                  label: "Textarea",
                  type: "textarea",
                  required: true,
                  placeholder: "Business justification",
                  minLength: 12,
                  maxLength: 240,
                  help: "Textarea bounded to keep generated workflow comments concise.",
                },
                {
                  id: "number",
                  label: "Number",
                  type: "number",
                  placeholder: "1",
                  min: 1,
                  max: 20,
                  step: 1,
                  defaultValue: 5,
                  help: "Numeric input constrained by min, max, and step.",
                },
                {
                  id: "money",
                  label: "Money",
                  type: "money",
                  placeholder: "112000",
                  prefix: "$",
                  min: 0,
                  max: 250000,
                  step: 500,
                  inputMode: "decimal",
                  help: "Currency-style number with prefix, bounds, and increment step.",
                },
                {
                  id: "percent",
                  label: "Percent",
                  type: "percent",
                  placeholder: "7.5",
                  suffix: "%",
                  min: 0,
                  max: 25,
                  step: 0.25,
                  inputMode: "decimal",
                  defaultValue: 7.5,
                  help: "Percentage-style number with suffix and fractional step.",
                },
                {
                  id: "date",
                  label: "Date",
                  type: "date",
                  min: "2026-05-15",
                  max: "2026-12-31",
                  defaultValue: "2026-06-01",
                  help: "Date input bounded to the active plan window.",
                },
                {
                  id: "effectiveDate",
                  label: "Effective date",
                  type: "date",
                  required: true,
                  min: "2026-06-01",
                  max: "2026-09-30",
                  defaultValue: "2026-06-15",
                },
                {
                  id: "dateRange",
                  label: "Date range",
                  type: "date_range",
                },
                {
                  id: "time",
                  label: "Time",
                  type: "time",
                  min: "08:00",
                  max: "18:00",
                  step: 900,
                  defaultValue: "09:30",
                  help: "Time input limited to 15-minute business-hour increments.",
                },
                {
                  id: "email",
                  label: "Email",
                  type: "email",
                  placeholder: "employee@example.com",
                  pattern: "^[^\\s@]+@harborcare\\.example$",
                  validationMessage: "Use the HarborCare employee email domain.",
                  help: "Email input with a domain-specific pattern.",
                },
                {
                  id: "phone",
                  label: "Phone",
                  type: "phone",
                  placeholder: "(555) 010-2040",
                  inputMode: "tel",
                  maxLength: 14,
                  pattern: "^\\([0-9]{3}\\) [0-9]{3}-[0-9]{4}$",
                  transform: "phone_us",
                  validationMessage: "Use (555) 010-2040 format.",
                  help: "Phone input normalizes digits into a US phone shape.",
                },
                {
                  id: "url",
                  label: "URL",
                  type: "url",
                  placeholder: "https://example.com/policy",
                  pattern: "^https://.*$",
                  validationMessage: "Only HTTPS links are allowed.",
                  help: "URL input constrained to HTTPS.",
                },
                {
                  id: "limitedText",
                  label: "Limited label",
                  type: "text",
                  placeholder: "Reviewer note label",
                  minLength: 3,
                  maxLength: 24,
                  help: "Short label field with a tight character budget.",
                },
                {
                  id: "employeeCode",
                  label: "Employee code",
                  type: "text",
                  placeholder: "HR-1048",
                  maxLength: 7,
                  pattern: "^[A-Z]{2}-[0-9]{4}$",
                  transform: "alpha_numeric_upper",
                  validationMessage: "Use two letters, a dash, and four digits.",
                  help: "Pattern-enforced structured identifier.",
                },
                {
                  id: "workflowSlug",
                  label: "Workflow slug",
                  type: "text",
                  placeholder: "manager-comp-review",
                  maxLength: 32,
                  pattern: "^[a-z0-9-]+$",
                  transform: "slug",
                  help: "Slug normalizes text to lowercase URL-safe tokens.",
                },
                {
                  id: "lastFour",
                  label: "Identifier last four",
                  type: "text",
                  placeholder: "1234",
                  inputMode: "numeric",
                  minLength: 4,
                  maxLength: 4,
                  pattern: "^[0-9]{4}$",
                  transform: "digits_only",
                  validationMessage: "Enter exactly four digits.",
                  help: "Digits-only field with exact length enforcement.",
                },
                {
                  id: "postalCode",
                  label: "Postal code",
                  type: "text",
                  placeholder: "02139 or 02139-1234",
                  inputMode: "numeric",
                  maxLength: 10,
                  pattern: "^[0-9]{5}(-[0-9]{4})?$",
                  validationMessage: "Use ZIP or ZIP+4 format.",
                  help: "Optional extension format enforced by pattern.",
                },
                {
                  id: "systemRequestId",
                  label: "System request ID",
                  type: "text",
                  defaultValue: "WF-2026-1048",
                  readOnly: true,
                  help: "Read-only generated value that still submits in payload.",
                },
                {
                  id: "sourceSystem",
                  label: "Source system",
                  type: "text",
                  defaultValue: "Workday",
                  disabled: true,
                  help: "Disabled value shown for context but not directly editable.",
                },
              ],
            },
          },
          {
            id: "choice-form-controls",
            type: "form.dynamicFieldGroup",
            title: "Choice and upload controls",
            props: {
              fields: [
                {
                  id: "select",
                  label: "Select",
                  type: "select",
                  options: [
                    { label: "Low risk", value: "low" },
                    { label: "Medium risk", value: "medium" },
                    { label: "High risk", value: "high" },
                  ],
                },
                {
                  id: "dropdown",
                  label: "Dropdown",
                  type: "dropdown",
                  options: [
                    { label: "Manager request", value: "manager_request" },
                    { label: "HRBP approval", value: "hrbp_approval" },
                    { label: "Payroll review", value: "payroll_review" },
                  ],
                },
                {
                  id: "groupedDropdown",
                  label: "Grouped dropdown",
                  type: "grouped_select",
                  options: [
                    {
                      label: "Compensation change",
                      value: "comp_change",
                      group: "Employee changes",
                    },
                    {
                      label: "Manager change",
                      value: "manager_change",
                      group: "Employee changes",
                    },
                    {
                      label: "Headcount requisition",
                      value: "headcount",
                      group: "Position changes",
                    },
                    {
                      label: "Backfill approval",
                      value: "backfill",
                      group: "Position changes",
                    },
                  ],
                },
                {
                  id: "filterableDropdown",
                  label: "Filterable dropdown",
                  type: "filterable_select",
                  options: [
                    {
                      label: "Jane Rivera",
                      value: "emp_jane_rivera",
                      group: "Nursing",
                    },
                    {
                      label: "Mina Patel",
                      value: "emp_mina_patel",
                      group: "Nursing",
                    },
                    {
                      label: "Owen Chen",
                      value: "emp_owen_chen",
                      group: "Operations",
                    },
                    {
                      label: "Priya Shah",
                      value: "emp_priya_shah",
                      group: "Finance",
                    },
                  ],
                },
                {
                  id: "multiSelect",
                  label: "Multi-select",
                  type: "multi_select",
                  options: [
                    { label: "HRBP", value: "hrbp" },
                    { label: "Compensation", value: "compensation" },
                    { label: "Payroll", value: "payroll" },
                    { label: "Finance", value: "finance" },
                  ],
                },
                {
                  id: "radio",
                  label: "Radio group",
                  type: "radio",
                  options: [
                    { label: "Sequential approval", value: "sequential" },
                    { label: "Parallel approval", value: "parallel" },
                  ],
                },
                {
                  id: "radioGroup",
                  label: "Grouped radio buttons",
                  type: "radio_group",
                  options: [
                    {
                      label: "Manager only",
                      value: "manager_only",
                      group: "Simple",
                    },
                    {
                      label: "Manager + HRBP",
                      value: "manager_hrbp",
                      group: "Simple",
                    },
                    {
                      label: "HRBP + Compensation + Finance",
                      value: "cross_functional",
                      group: "Complex",
                    },
                    {
                      label: "Sequential leadership chain",
                      value: "leadership_chain",
                      group: "Complex",
                    },
                  ],
                },
                {
                  id: "checkbox",
                  label: "Checkbox",
                  type: "checkbox",
                  placeholder: "Include downstream access review",
                },
                {
                  id: "toggle",
                  label: "Toggle",
                  type: "toggle",
                  placeholder: "Enable compact reviewer view",
                },
                {
                  id: "toggleGroup",
                  label: "Toggle group",
                  type: "toggle_group",
                  options: [
                    {
                      label: "Notify HRBP",
                      value: "notifyHrbp",
                      description: "Workflow notifications",
                      defaultValue: true,
                    },
                    {
                      label: "Require finance review",
                      value: "requireFinance",
                      description: "Approval routing",
                    },
                    {
                      label: "Show employee-facing copy",
                      value: "employeeCopy",
                      description: "Surface personalization",
                      defaultValue: true,
                    },
                  ],
                },
                {
                  id: "slider",
                  label: "Slider",
                  type: "slider",
                  help: "Used for thresholds such as quorum percentage.",
                },
                {
                  id: "sliderGroup",
                  label: "Metric sliders",
                  type: "slider_group",
                  options: [
                    {
                      label: "Policy fit",
                      value: "policyFit",
                      description: "Reviewer confidence",
                      defaultValue: 82,
                    },
                    {
                      label: "Cost impact",
                      value: "costImpact",
                      description: "Finance sensitivity",
                      defaultValue: 42,
                    },
                    {
                      label: "Execution readiness",
                      value: "executionReadiness",
                      description: "Preflight confidence",
                      defaultValue: 68,
                    },
                  ],
                },
                {
                  id: "file",
                  label: "File upload",
                  type: "file",
                },
                {
                  id: "evidence",
                  label: "Evidence upload",
                  type: "evidence_upload",
                },
                {
                  id: "repeatingList",
                  label: "Repeating list",
                  type: "repeating_list",
                },
                {
                  id: "tableEditor",
                  label: "Table editor",
                  type: "table_editor",
                },
                {
                  id: "policyAck",
                  label: "Policy acknowledgement",
                  type: "policy_acknowledgement",
                  placeholder: "I reviewed the compensation and transfer policy.",
                },
                {
                  id: "signature",
                  label: "E-signature / attestation",
                  type: "signature",
                },
                {
                  id: "signatureCapture",
                  label: "Signature capture",
                  type: "signature_capture",
                  help: "Supports typed, drawn, or uploaded signatures.",
                },
                {
                  id: "matrix",
                  label: "Matrix",
                  type: "matrix",
                  rows: [
                    { label: "Policy fit", value: "policyFit" },
                    { label: "Business impact", value: "businessImpact" },
                    { label: "Execution readiness", value: "executionReadiness" },
                  ],
                  columns: [
                    { label: "Low", value: "low" },
                    { label: "Medium", value: "medium" },
                    { label: "High", value: "high" },
                  ],
                },
                {
                  id: "clusterBoard",
                  label: "Drag/drop clustering board",
                  type: "cluster_board",
                  help: "Drag cards between groups to define workflow clusters or page sections.",
                  groups: [
                    { label: "Intake", value: "Intake" },
                    { label: "Review", value: "Review" },
                    { label: "Execution", value: "Execution" },
                  ],
                  items: [
                    {
                      label: "Employee picker",
                      value: "employeePicker",
                      group: "Intake",
                      description: "Collect workflow subject",
                    },
                    {
                      label: "Evidence upload",
                      value: "evidenceUpload",
                      group: "Intake",
                      description: "Collect supporting files",
                    },
                    {
                      label: "Risk matrix",
                      value: "riskMatrix",
                      group: "Review",
                      description: "Reviewer scoring",
                    },
                    {
                      label: "Approval buttons",
                      value: "approvalButtons",
                      group: "Execution",
                      description: "Governed transition controls",
                    },
                  ],
                },
              ],
            },
          },
          {
            id: "domain-form-controls",
            type: "form.dynamicFieldGroup",
            title: "HCM domain controls",
            props: {
              fields: [
                {
                  id: "employee",
                  label: "Employee picker",
                  type: "employee_picker",
                  options: [
                    { label: "Jane Rivera", value: "emp_jane_rivera" },
                    { label: "Mina Patel", value: "emp_mina_patel" },
                  ],
                },
                {
                  id: "manager",
                  label: "Manager picker",
                  type: "manager_picker",
                  options: [
                    { label: "Alex Manager", value: "actor_manager_alex" },
                    { label: "Jordan Lee", value: "actor_jordan_lee" },
                  ],
                },
                {
                  id: "orgUnit",
                  label: "Org unit picker",
                  type: "org_unit_picker",
                  options: [
                    { label: "Clinical Operations", value: "clinical_ops" },
                    { label: "People Operations", value: "people_ops" },
                  ],
                },
                {
                  id: "department",
                  label: "Department picker",
                  type: "department_picker",
                  options: [
                    { label: "Cambridge Nursing", value: "cambridge_nursing" },
                    { label: "Somerville Nursing", value: "somerville_nursing" },
                  ],
                },
                {
                  id: "legalEntity",
                  label: "Legal entity picker",
                  type: "legal_entity_picker",
                  options: [
                    { label: "HarborCare US", value: "harborcare_us" },
                    { label: "HarborCare Services", value: "harborcare_services" },
                  ],
                },
                {
                  id: "costCenter",
                  label: "Cost center picker",
                  type: "cost_center_picker",
                  options: [
                    { label: "CC-401 Clinical", value: "cc_401" },
                    { label: "CC-220 Shared Services", value: "cc_220" },
                  ],
                },
                {
                  id: "location",
                  label: "Location picker",
                  type: "location_picker",
                  options: [
                    { label: "Cambridge, MA", value: "cambridge_ma" },
                    { label: "Somerville, MA", value: "somerville_ma" },
                  ],
                },
                {
                  id: "jobProfile",
                  label: "Job profile picker",
                  type: "job_profile_picker",
                  options: [
                    { label: "Registered Nurse", value: "rn" },
                    { label: "Senior Registered Nurse", value: "sr_rn" },
                  ],
                },
                {
                  id: "position",
                  label: "Position picker",
                  type: "position_picker",
                  options: [
                    { label: "RN-1820", value: "rn_1820" },
                    { label: "RN-1904", value: "rn_1904" },
                  ],
                },
                {
                  id: "payBand",
                  label: "Pay band picker",
                  type: "pay_band_picker",
                  options: [
                    { label: "Nurse Band 3", value: "nurse_band_3" },
                    { label: "Nurse Band 4", value: "nurse_band_4" },
                  ],
                },
                {
                  id: "compensationEditor",
                  label: "Compensation editor",
                  type: "compensation_editor",
                },
                {
                  id: "jobChangeEditor",
                  label: "Job change editor",
                  type: "job_change_editor",
                },
                {
                  id: "managerOrgEditor",
                  label: "Manager/org change editor",
                  type: "manager_org_change_editor",
                },
                {
                  id: "workerAssignmentEditor",
                  label: "Worker assignment editor",
                  type: "worker_assignment_editor",
                },
                {
                  id: "roleBindingEditor",
                  label: "Role binding editor",
                  type: "role_binding_editor",
                },
                {
                  id: "effectiveDatedFact",
                  label: "Effective-dated fact editor",
                  type: "effective_dated_fact_editor",
                },
                {
                  id: "payrollCutoff",
                  label: "Payroll cutoff picker",
                  type: "payroll_cutoff_picker",
                  options: [
                    { label: "May 31 cutoff", value: "2026-05-31" },
                    { label: "June 15 cutoff", value: "2026-06-15" },
                  ],
                },
              ],
            },
          },
          {
            id: "entity-access-controls",
            type: "form.dynamicFieldGroup",
            title: "Entity access controls",
            props: {
              fields: [
                {
                  id: "permissionAwareEmployee",
                  label: "Permission-aware entity picker",
                  type: "permission_entity_picker",
                  options: [
                    {
                      label: "Jane Rivera",
                      value: "emp_jane_rivera",
                      group: "Cambridge Nursing",
                      description: "Senior Registered Nurse",
                      scope: "Direct report",
                      permission: "employee.profile.view",
                      status: "success",
                    },
                    {
                      label: "Mina Patel",
                      value: "emp_mina_patel",
                      group: "Cambridge Nursing",
                      description: "Registered Nurse",
                      scope: "Department scope",
                      permission: "employee.profile.view",
                      status: "success",
                    },
                    {
                      label: "Priya Shah",
                      value: "emp_priya_shah",
                      group: "Finance",
                      description: "Finance partner",
                      scope: "Workflow collaborator",
                      permission: "workflow.approve",
                      status: "info",
                    },
                  ],
                },
                {
                  id: "managerTree",
                  label: "Org chart / manager tree picker",
                  type: "manager_tree_picker",
                  options: [
                    {
                      label: "Avery Stone",
                      value: "actor_avery_stone",
                      group: "Executive",
                      description: "VP People Operations",
                      level: 0,
                      status: "success",
                    },
                    {
                      label: "Alex Manager",
                      value: "actor_manager_alex",
                      group: "Clinical",
                      description: "Director, Cambridge Nursing",
                      level: 1,
                      status: "success",
                    },
                    {
                      label: "Jordan Lee",
                      value: "actor_jordan_lee",
                      group: "Clinical",
                      description: "Nurse Manager",
                      level: 2,
                      status: "info",
                    },
                    {
                      label: "Riley HRBP",
                      value: "actor_riley_hrbp",
                      group: "People",
                      description: "HRBP support relationship",
                      level: 1,
                      status: "warning",
                    },
                  ],
                },
                {
                  id: "orgTreePicker",
                  label: "Org tree picker",
                  type: "org_tree_picker",
                  options: [
                    {
                      label: "HarborCare",
                      value: "org_harborcare",
                      group: "Company",
                      description: "Tenant root",
                      level: 0,
                      status: "success",
                    },
                    {
                      label: "Clinical Operations",
                      value: "org_clinical",
                      group: "Division",
                      description: "Patient care delivery",
                      level: 1,
                      status: "success",
                    },
                    {
                      label: "Cambridge Nursing",
                      value: "org_cambridge_nursing",
                      group: "Department",
                      description: "Workflow subject department",
                      level: 2,
                      status: "info",
                    },
                    {
                      label: "People Operations",
                      value: "org_people_ops",
                      group: "Division",
                      description: "HR and compliance support",
                      level: 1,
                      status: "warning",
                    },
                  ],
                },
              ],
            },
          },
          {
            id: "effective-change-controls",
            type: "form.dynamicFieldGroup",
            title: "Effective dating and change controls",
            props: {
              fields: [
                {
                  id: "effectiveDatedChange",
                  label: "Effective-dated change",
                  type: "effective_dated_change",
                  help: "Captures future, retroactive, and correction timing with payroll cutoff awareness.",
                },
                {
                  id: "beforeAfterEditor",
                  label: "Before/after field editor",
                  type: "before_after_field_editor",
                  current: "Registered Nurse",
                  proposed: "Senior Registered Nurse",
                },
              ],
            },
          },
          {
            id: "compensation-schedule-controls",
            type: "form.dynamicFieldGroup",
            title: "Compensation and schedule controls",
            props: {
              fields: [
                {
                  id: "compPackage",
                  label: "Compensation package editor",
                  type: "compensation_package_editor",
                  basePay: 112000,
                  bonusTarget: 10,
                },
                {
                  id: "scheduleTime",
                  label: "Schedule and time control",
                  type: "schedule_time_control",
                },
              ],
            },
          },
          {
            id: "approval-policy-controls",
            type: "form.dynamicFieldGroup",
            title: "Approval, policy, and AI controls",
            props: {
              fields: [
                {
                  id: "approvalChain",
                  label: "Approval chain preview/editor",
                  type: "approval_chain_editor",
                  items: [
                    {
                      label: "Manager approval",
                      value: "manager",
                      approver: "Alex Manager",
                      rule: "Required first approval",
                      status: "Approved",
                    },
                    {
                      label: "HRBP approval",
                      value: "hrbp",
                      approver: "Riley HRBP",
                      rule: "Required for org and pay changes",
                      status: "Pending",
                    },
                    {
                      label: "Finance approval",
                      value: "finance",
                      approver: "Priya Shah",
                      rule: "Required when budget changes",
                      status: "Pending",
                    },
                  ],
                },
                {
                  id: "policyEvidence",
                  label: "Policy and evidence checklist",
                  type: "policy_evidence_checklist",
                  items: [
                    {
                      label: "Business justification",
                      value: "businessJustification",
                      description: "Required for audit review",
                      status: "warning",
                      defaultValue: true,
                    },
                    {
                      label: "Compensation band check",
                      value: "compBandCheck",
                      description: "Must pass before approval",
                      status: "warning",
                    },
                    {
                      label: "Employee notification copy",
                      value: "employeeNotification",
                      description: "Required before execution",
                      status: "info",
                    },
                  ],
                },
                {
                  id: "aiReview",
                  label: "AI review panel",
                  type: "ai_review_panel",
                  items: [
                    {
                      label: "Missing data",
                      value: "missingData",
                      description: "No required fields missing",
                      status: "success",
                    },
                    {
                      label: "Payroll risk",
                      value: "payrollRisk",
                      description: "Effective date is near cutoff",
                      status: "warning",
                    },
                    {
                      label: "Visibility",
                      value: "visibility",
                      description: "AI saw only permitted employee fields",
                      status: "info",
                    },
                  ],
                },
              ],
            },
          },
          {
            id: "bulk-repair-controls",
            type: "form.dynamicFieldGroup",
            title: "Bulk, conflict, and repair controls",
            props: {
              fields: [
                {
                  id: "bulkGrid",
                  label: "Bulk grid editor",
                  type: "bulk_grid_editor",
                  columns: [
                    { label: "Employee", value: "employee" },
                    { label: "Field", value: "field" },
                    { label: "Current", value: "current" },
                    { label: "Proposed", value: "proposed" },
                  ],
                  rows: [
                    {
                      employee: "Jane Rivera",
                      field: "Department",
                      current: "Med Surg",
                      proposed: "ICU",
                    },
                    {
                      employee: "Mina Patel",
                      field: "Manager",
                      current: "Jordan Lee",
                      proposed: "Alex Manager",
                    },
                  ],
                },
                {
                  id: "conflictResolver",
                  label: "Conflict resolver",
                  type: "conflict_resolver",
                  items: [
                    {
                      label: "Future-dated manager change",
                      value: "futureManager",
                      description: "A competing manager change already exists",
                      defaultResolution: "review",
                    },
                    {
                      label: "Payroll amount mismatch",
                      value: "payrollMismatch",
                      description: "Payroll export differs from current projection",
                      defaultResolution: "use_current",
                    },
                  ],
                },
                {
                  id: "integrationRepair",
                  label: "Integration status and repair",
                  type: "integration_repair_control",
                  items: [
                    {
                      label: "UKG writeback",
                      value: "ukgWriteback",
                      description: "HTTP 409 version conflict",
                      status: "error",
                      defaultAction: "manual_repair",
                    },
                    {
                      label: "Payroll export",
                      value: "payrollExport",
                      description: "Queued after cutoff validation",
                      status: "warning",
                      defaultAction: "retry",
                    },
                  ],
                },
              ],
            },
          },
          {
            id: "privacy-region-simulation-controls",
            type: "form.dynamicFieldGroup",
            title: "Privacy, regional, and simulation controls",
            props: {
              fields: [
                {
                  id: "sensitiveReveal",
                  label: "Sensitive field reveal",
                  type: "sensitive_field_reveal",
                  placeholder: "$112,000 base pay",
                },
                {
                  id: "internationalContact",
                  label: "International address/contact",
                  type: "international_contact",
                },
                {
                  id: "transactionSimulation",
                  label: "Transaction simulation viewer",
                  type: "transaction_simulation_viewer",
                  items: [
                    {
                      label: "Update employee projection",
                      value: "employeeProjection",
                      description: "Writes current employee view",
                      status: "success",
                    },
                    {
                      label: "Create payroll export",
                      value: "payrollExport",
                      description: "Pending payroll cutoff review",
                      status: "warning",
                    },
                    {
                      label: "Sync identity group",
                      value: "identitySync",
                      description: "Queued after HCM write succeeds",
                      status: "info",
                    },
                  ],
                },
              ],
            },
          },
        ],
      },
    ],
  },
  {
    id: "example-all-widgets",
    title: "All Widgets Example",
    description:
      "Gallery page showing the current registered workflow, content, media, and data display widgets.",
    workflowTypes: ["*"],
    surfaceModes: ["full_app", "admin_preview", "mobile_compact"],
    regions: [
      {
        id: "widget-gallery",
        layout: "grid",
        width: "wide",
        widgets: [
          {
            id: "widget-callout",
            type: "content.callout",
            title: "Callout",
            props: {
              body: "Use callouts for state-specific guidance, policy warnings, and reviewer cues.",
            },
          },
          {
            id: "widget-section-layout",
            type: "layout.section",
            title: "Section layout",
            props: {
              body: "Section layout gives generated pages a branded, titled content region.",
            },
          },
          {
            id: "widget-stack-layout",
            type: "layout.stack",
            title: "Stack layout",
            props: {
              body: "Stack layout arranges generated workflow content in a single column.",
            },
          },
          {
            id: "widget-grid-layout",
            type: "layout.grid",
            title: "Grid layout",
            props: {
              body: "Grid layout provides a responsive multi-column surface for atomic widgets.",
            },
          },
          {
            id: "widget-text",
            type: "content.text",
            title: "Text block",
            props: {
              body: "Plain text content can be hard-coded, API-backed, localized, or hidden by rules.",
            },
          },
          {
            id: "widget-markdown",
            type: "content.markdown",
            title: "Markdown viewer",
            props: {
              markdown:
                "Markdown is useful for customer-authored instructions and policy snippets.",
            },
          },
          {
            id: "widget-html",
            type: "content.html",
            title: "Sanitized HTML viewer",
            props: {
              html: "<strong>Safe subset:</strong> scripts and unsafe event handlers are stripped.",
            },
          },
          {
            id: "widget-link-list",
            type: "content.linkList",
            title: "Link list",
            props: {
              links: [
                { label: "Policy center", href: "#" },
                { label: "Compensation guide", href: "#" },
                { label: "Payroll calendar", href: "#" },
              ],
            },
          },
          {
            id: "widget-metric",
            type: "content.metricTile",
            title: "Metric tile",
            props: {
              label: "Open approvals",
              value: 18,
              detail: "Across HRBP and compensation queues",
            },
          },
          {
            id: "widget-progress",
            type: "data.progress",
            title: "Progress",
            props: {
              value: 68,
            },
          },
          {
            id: "widget-metric-graph",
            type: "data.metricGraph",
            title: "Metric graph",
            props: {
              variant: "bar",
              points: [
                { label: "Mon", value: 12, target: 10 },
                { label: "Tue", value: 18, target: 14 },
                { label: "Wed", value: 9, target: 12 },
                { label: "Thu", value: 22, target: 16 },
                { label: "Fri", value: 16, target: 15 },
              ],
            },
          },
          {
            id: "widget-graph-chart",
            type: "data.graphChart",
            title: "Graph chart",
            props: {
              series: [
                {
                  id: "approvals",
                  label: "Approvals",
                  points: [
                    { label: "Mon", value: 8 },
                    { label: "Tue", value: 14 },
                    { label: "Wed", value: 11 },
                    { label: "Thu", value: 19 },
                    { label: "Fri", value: 16 },
                  ],
                },
                {
                  id: "exceptions",
                  label: "Exceptions",
                  points: [
                    { label: "Mon", value: 3 },
                    { label: "Tue", value: 6 },
                    { label: "Wed", value: 4 },
                    { label: "Thu", value: 8 },
                    { label: "Fri", value: 5 },
                  ],
                },
              ],
            },
          },
          {
            id: "widget-node-graph",
            type: "data.nodeGraph",
            title: "Node graph",
            props: {
              nodes: [
                {
                  id: "intake",
                  label: "Intake",
                  group: "Workflow",
                  description: "Manager request",
                  x: 14,
                  y: 50,
                  status: "success",
                },
                {
                  id: "policy",
                  label: "Policy",
                  group: "Validation",
                  description: "Rule evaluation",
                  x: 38,
                  y: 24,
                  status: "info",
                },
                {
                  id: "comp",
                  label: "Comp",
                  group: "Review",
                  description: "Band fit",
                  x: 62,
                  y: 36,
                  status: "warning",
                },
                {
                  id: "payroll",
                  label: "Payroll",
                  group: "Execution",
                  description: "Downstream write",
                  x: 84,
                  y: 58,
                  status: "success",
                },
                {
                  id: "audit",
                  label: "Audit",
                  group: "Ledger",
                  description: "Event trail",
                  x: 58,
                  y: 78,
                  status: "info",
                },
              ],
              edges: [
                { source: "intake", target: "policy", label: "validate" },
                { source: "policy", target: "comp", label: "route" },
                { source: "comp", target: "payroll", label: "approve" },
                { source: "payroll", target: "audit", label: "write" },
                { source: "intake", target: "audit", label: "event" },
              ],
            },
          },
          {
            id: "widget-org-chart",
            type: "data.orgChart",
            title: "Org chart",
            props: {
              nodes: [
                {
                  id: "avery",
                  label: "Avery Stone",
                  description: "VP People Operations",
                  status: "success",
                },
                {
                  id: "alex",
                  parentId: "avery",
                  label: "Alex Manager",
                  description: "Director, Cambridge Nursing",
                  status: "success",
                },
                {
                  id: "jordan",
                  parentId: "alex",
                  label: "Jordan Lee",
                  description: "Nurse Manager",
                  status: "info",
                },
                {
                  id: "riley",
                  parentId: "avery",
                  label: "Riley HRBP",
                  description: "People partner",
                  status: "warning",
                },
                {
                  id: "finance",
                  parentId: "avery",
                  label: "Priya Shah",
                  description: "Finance reviewer",
                  status: "info",
                },
              ],
            },
          },
          {
            id: "widget-table",
            type: "data.table",
            title: "Table",
            props: {
              columns: [
                { id: "field", label: "Field" },
                { id: "current", label: "Current" },
                { id: "proposed", label: "Proposed" },
              ],
              rows: [
                {
                  id: "manager",
                  field: "Manager",
                  current: "Alex Manager",
                  proposed: "Jordan Lee",
                },
                {
                  id: "department",
                  field: "Department",
                  current: "Somerville Nursing",
                  proposed: "Cambridge Nursing",
                },
                {
                  id: "compensation",
                  field: "Compensation",
                  current: "$105,000",
                  proposed: "$112,000",
                },
              ],
            },
          },
          {
            id: "widget-filterable-table",
            type: "data.filterableTable",
            title: "Filterable table",
            props: {
              columns: [
                { id: "request", label: "Request" },
                { id: "owner", label: "Owner" },
                { id: "risk", label: "Risk" },
                { id: "state", label: "State" },
              ],
              rows: [
                {
                  id: "req-1",
                  request: "Promotion package",
                  owner: "Compensation",
                  risk: "medium",
                  state: "Waiting review",
                },
                {
                  id: "req-2",
                  request: "Headcount requisition",
                  owner: "Finance",
                  risk: "high",
                  state: "Waiting approval",
                },
                {
                  id: "req-3",
                  request: "Emergency contact update",
                  owner: "HR Ops",
                  risk: "low",
                  state: "Ready to execute",
                },
              ],
            },
          },
          {
            id: "widget-matrix",
            type: "data.matrix",
            title: "Matrix",
            props: {
              rows: [
                { label: "Policy fit", value: "policyFit" },
                { label: "Cost impact", value: "costImpact" },
                { label: "Execution risk", value: "executionRisk" },
              ],
              columns: [
                { label: "Low", value: "low" },
                { label: "Medium", value: "medium" },
                { label: "High", value: "high" },
              ],
              values: {
                policyFit: "low",
                costImpact: "medium",
                executionRisk: "high",
              },
            },
          },
          {
            id: "widget-cluster-board",
            type: "data.clusterBoard",
            title: "Drag/drop cluster board",
            props: {
              groups: [
                { label: "Unassigned", value: "Unassigned" },
                { label: "Manager lane", value: "Manager lane" },
                { label: "HRBP lane", value: "HRBP lane" },
              ],
              items: [
                {
                  label: "Compensation review",
                  value: "compReview",
                  group: "Unassigned",
                  description: "Needs owner",
                },
                {
                  label: "Manager attestation",
                  value: "managerAttestation",
                  group: "Manager lane",
                  description: "Owner: manager",
                },
                {
                  label: "Policy exception",
                  value: "policyException",
                  group: "HRBP lane",
                  description: "Owner: HRBP",
                },
              ],
            },
          },
          {
            id: "widget-label-values",
            type: "content.labelValueList",
            title: "Label/value list",
            props: {
              items: [
                { label: "Workflow", value: "Org transfer + comp" },
                { label: "State", value: "Waiting approval" },
                { label: "Risk", value: "Medium" },
              ],
            },
          },
          {
            id: "widget-faq",
            type: "content.faq",
            title: "FAQ",
            props: {
              items: [
                {
                  question: "Can this page change approval rules?",
                  answer:
                    "No. The page can render actions, but server workflow transitions remain authoritative.",
                },
                {
                  question: "Can content widgets reveal sensitive fields?",
                  answer:
                    "No. API-backed content still goes through the binding and permission resolver.",
                },
              ],
            },
          },
          {
            id: "widget-request-list",
            type: "queue.requestList",
            title: "Request queue",
            props: {
              requests: [
                {
                  id: "wf-100",
                  title: "Promotion package",
                  employee: "Jane Rivera",
                  state: "Waiting compensation",
                  risk: "Medium",
                  due: "Today",
                },
                {
                  id: "wf-101",
                  title: "Headcount requisition",
                  employee: "Cambridge Nursing",
                  state: "Waiting finance",
                  risk: "High",
                  due: "Tomorrow",
                },
              ],
            },
          },
          {
            id: "widget-employee",
            type: "employee.summary",
            title: "Employee summary",
            bindings: {
              employee: {
                source: "employee_projection",
              },
            },
          },
          {
            id: "widget-fields",
            type: "form.dynamicFieldGroup",
            title: "Dynamic field group",
            props: {
              fields: [
                {
                  id: "fieldA",
                  label: "Generated field",
                  type: "text",
                  placeholder: "Workflow-owned input",
                },
                {
                  id: "fieldB",
                  label: "Generated date",
                  type: "date",
                },
              ],
            },
          },
          {
            id: "widget-diff",
            type: "change.diff",
            title: "Current vs proposed diff",
            bindings: {
              current: {
                source: "workflow_context",
                path: "current",
              },
              proposed: {
                source: "workflow_input",
                path: "proposed",
              },
            },
          },
          {
            id: "widget-approval",
            type: "approval.decisionPanel",
            title: "Approval panel",
            actions: [
              {
                action: "approve",
                label: "Approve",
                transition: "approve",
                variant: "primary",
              },
              {
                action: "reject",
                label: "Reject",
                transition: "reject",
                variant: "danger",
              },
            ],
          },
          {
            id: "widget-reason-capture",
            type: "workflow.reasonCapture",
            title: "Reason capture",
            props: {
              label: "Decision reason",
              placeholder: "Add context for the approval record",
              required: true,
              reasons: ["Policy exception", "Missing evidence", "Payroll cutoff"],
            },
          },
          {
            id: "widget-simulation",
            type: "simulation.resultPanel",
            title: "Simulation panel",
            props: {
              checks: [
                {
                  label: "Policy",
                  status: "success",
                  detail: "No blocking policy conflicts.",
                },
                {
                  label: "Payroll",
                  status: "warning",
                  detail: "Effective date is near cutoff.",
                },
              ],
            },
          },
          {
            id: "widget-timeline",
            type: "audit.timeline",
            title: "Audit timeline",
            props: {
              events: [
                {
                  at: "2026-05-15T09:05:00Z",
                  actor: "Alex Manager",
                  label: "Submitted",
                },
                {
                  at: "2026-05-15T10:00:00Z",
                  actor: "Riley HRBP",
                  label: "Reviewed",
                },
              ],
            },
          },
          {
            id: "widget-ai",
            type: "ai.changeBrief",
            title: "AI change brief",
            props: {
              summary:
                "AI summary explains risk, missing data, downstream impact, and permitted visibility.",
              visibility:
                "AI saw only fields permitted for this workflow and actor context.",
            },
          },
          {
            id: "widget-image",
            type: "media.image",
            title: "Image",
            props: {
              src: "https://images.unsplash.com/photo-1551836022-d5d88e9218df?auto=format&fit=crop&w=1200&q=80",
              alt: "Operational team review",
            },
          },
          {
            id: "widget-audio",
            type: "media.audio",
            title: "Audio player",
            props: {
              src: "",
              transcript: "Audio placeholder for workflow instructions.",
            },
          },
          {
            id: "widget-video",
            type: "media.video",
            title: "Video player",
            props: {
              src: "",
              transcript: "Video placeholder for training or customer guidance.",
            },
          },
          {
            id: "widget-pdf",
            type: "media.pdf",
            title: "PDF preview",
            props: {
              src: "",
              label: "PDF preview placeholder",
            },
          },
        ],
      },
    ],
  },
  {
    id: "example-professional-blend",
    title: "Professional Blend Example",
    description:
      "A realistic branded workflow page combining operational controls, governed workflow widgets, and benign content.",
    workflowTypes: ["employee.org_transfer_compensation_change"],
    surfaceModes: ["full_app", "customer_portal", "admin_preview", "mobile_compact"],
    regions: [
      {
        id: "professional-main",
        layout: "grid",
        width: "wide",
        widgets: [
          {
            id: "blend-employee",
            type: "employee.summary",
            title: "Employee context",
            bindings: {
              employee: {
                source: "employee_projection",
              },
            },
          },
          {
            id: "blend-sla",
            type: "content.metricTile",
            title: "SLA",
            props: {
              label: "Approval SLA",
              value: "18h",
              detail: "Before compensation escalation",
            },
          },
          {
            id: "blend-throughput-graph",
            type: "data.metricGraph",
            title: "Approval throughput",
            props: {
              variant: "line",
              points: [
                { label: "HRBP", value: 14, target: 12 },
                { label: "Comp", value: 9, target: 10 },
                { label: "Finance", value: 6, target: 8 },
                { label: "Payroll", value: 11, target: 9 },
              ],
            },
          },
          {
            id: "blend-guidance",
            type: "content.callout",
            title: "Reviewer guidance",
            props: {
              body: "Review the proposed manager, department, effective date, compensation band, and simulation results before making a decision.",
            },
          },
          {
            id: "blend-form",
            type: "form.dynamicFieldGroup",
            title: "Decision inputs",
            props: {
              fields: [
                {
                  id: "approvalOutcome",
                  label: "Decision recommendation",
                  type: "radio",
                  required: true,
                  options: [
                    { label: "Approve", value: "approve" },
                    { label: "Request more info", value: "request_info" },
                    { label: "Reject", value: "reject" },
                  ],
                },
                {
                  id: "reviewerComment",
                  label: "Reviewer comment",
                  type: "textarea",
                  required: true,
                },
                {
                  id: "followUpDate",
                  label: "Follow-up date",
                  type: "date",
                },
                {
                  id: "routingOptions",
                  label: "Routing toggles",
                  type: "toggle_group",
                  options: [
                    {
                      label: "Escalate on SLA breach",
                      value: "escalateOnBreach",
                      description: "Controls notification routing",
                      defaultValue: true,
                    },
                    {
                      label: "Add payroll watcher",
                      value: "payrollWatcher",
                      description: "Adds read-only workflow visibility",
                    },
                  ],
                },
                {
                  id: "reviewSignals",
                  label: "Review signal sliders",
                  type: "slider_group",
                  options: [
                    {
                      label: "Business priority",
                      value: "businessPriority",
                      description: "Manager urgency",
                      defaultValue: 78,
                    },
                    {
                      label: "Policy confidence",
                      value: "policyConfidence",
                      description: "HRBP confidence",
                      defaultValue: 86,
                    },
                    {
                      label: "Payroll risk",
                      value: "payrollRisk",
                      description: "Execution sensitivity",
                      defaultValue: 34,
                    },
                  ],
                },
                {
                  id: "attachEvidence",
                  label: "Attach evidence",
                  type: "evidence_upload",
                },
              ],
            },
          },
          {
            id: "blend-diff",
            type: "change.diff",
            title: "Change detail",
            bindings: {
              current: {
                source: "workflow_context",
                path: "current",
              },
              proposed: {
                source: "workflow_input",
                path: "proposed",
              },
            },
          },
          {
            id: "blend-simulation",
            type: "simulation.resultPanel",
            title: "Pre-execution checks",
            props: {
              checks: [
                {
                  label: "Compensation band",
                  status: "success",
                  detail: "Proposed compensation is inside the permitted band.",
                },
                {
                  label: "Payroll cutoff",
                  status: "warning",
                  detail: "Effective date is within seven days of cutoff.",
                },
                {
                  label: "Access impact",
                  status: "info",
                  detail: "Department change updates access groups after execution.",
                },
              ],
            },
          },
          {
            id: "blend-progress",
            type: "data.progress",
            title: "Approval progress",
            props: {
              value: 66,
            },
          },
          {
            id: "blend-review-table",
            type: "data.filterableTable",
            title: "Reviewer queue context",
            props: {
              columns: [
                { id: "request", label: "Request" },
                { id: "owner", label: "Owner" },
                { id: "risk", label: "Risk" },
                { id: "state", label: "State" },
              ],
              rows: [
                {
                  id: "blend-row-1",
                  request: "Org transfer and compensation change",
                  owner: "Riley HRBP",
                  risk: "medium",
                  state: "Current item",
                },
                {
                  id: "blend-row-2",
                  request: "Pay band exception",
                  owner: "Compensation",
                  risk: "high",
                  state: "Waiting review",
                },
                {
                  id: "blend-row-3",
                  request: "Manager correction",
                  owner: "HR Ops",
                  risk: "low",
                  state: "Ready to execute",
                },
              ],
            },
          },
          {
            id: "blend-risk-matrix",
            type: "data.matrix",
            title: "Risk matrix",
            props: {
              rows: [
                { label: "Policy fit", value: "policyFit" },
                { label: "Payroll timing", value: "payrollTiming" },
                { label: "Access impact", value: "accessImpact" },
              ],
              columns: [
                { label: "Low", value: "low" },
                { label: "Medium", value: "medium" },
                { label: "High", value: "high" },
              ],
              values: {
                policyFit: "low",
                payrollTiming: "medium",
                accessImpact: "medium",
              },
            },
          },
          {
            id: "blend-cluster-board",
            type: "data.clusterBoard",
            title: "Workflow grouping",
            props: {
              groups: [
                { label: "Manager", value: "Manager" },
                { label: "HRBP", value: "HRBP" },
                { label: "Compensation", value: "Compensation" },
              ],
              items: [
                {
                  label: "Reviewer comment",
                  value: "reviewerComment",
                  group: "Manager",
                  description: "Decision input",
                },
                {
                  label: "Policy attestation",
                  value: "policyAttestation",
                  group: "HRBP",
                  description: "Required checkpoint",
                },
                {
                  label: "Pay band validation",
                  value: "payBandValidation",
                  group: "Compensation",
                  description: "Compensation checkpoint",
                },
              ],
            },
          },
          {
            id: "blend-approval",
            type: "approval.decisionPanel",
            title: "Final decision",
            actions: [
              {
                action: "approve",
                label: "Approve",
                transition: "approve",
                variant: "primary",
              },
              {
                action: "request_more_information",
                label: "Request info",
                transition: "request_more_information",
                variant: "secondary",
              },
              {
                action: "reject",
                label: "Reject",
                transition: "reject",
                requiresReason: true,
                variant: "danger",
              },
            ],
          },
          {
            id: "blend-timeline",
            type: "audit.timeline",
            title: "Recent activity",
            props: {
              events: [
                {
                  at: "2026-05-15T09:05:00Z",
                  actor: "Alex Manager",
                  label: "Submitted manager request",
                },
                {
                  at: "2026-05-15T09:30:00Z",
                  actor: "System",
                  label: "Preflight completed",
                },
                {
                  at: "2026-05-15T10:15:00Z",
                  actor: "Riley HRBP",
                  label: "Opened approval review",
                },
              ],
            },
          },
          {
            id: "blend-ai",
            type: "ai.changeBrief",
            title: "AI brief",
            props: {
              summary:
                "Medium-risk move. Key review points are payroll timing, compensation band fit, and downstream access updates.",
              visibility:
                "AI visibility is limited to proposed job, department, effective date, permitted compensation band, and policy findings.",
            },
          },
        ],
      },
    ],
  },
];

type CatalogRecord = Readonly<Record<string, unknown>>;

const isCatalogRecord = (value: unknown): value is CatalogRecord =>
  typeof value === "object" && value !== null && !Array.isArray(value);

const catalogSurfaces = ["full_app", "admin_preview", "mobile_compact"] as const;

const findBaseWidget = (pageId: string, widgetId: string): WidgetInstance | undefined =>
  basePageDefinitions
    .find((page) => page.id === pageId)
    ?.regions.flatMap((region) => region.widgets)
    .find((widget) => widget.id === widgetId);

const sourceFields = (
  sourceWidgetId: string,
  fieldIds?: readonly string[],
): readonly CatalogRecord[] => {
  const fields = findBaseWidget("example-all-controls", sourceWidgetId)?.props?.fields;
  const allFields = Array.isArray(fields) ? fields.filter(isCatalogRecord) : [];

  if (fieldIds === undefined) {
    return allFields;
  }

  const selectedIds = new Set(fieldIds);

  return allFields.filter(
    (field) => typeof field.id === "string" && selectedIds.has(field.id),
  );
};

const fieldCatalogWidget = (
  id: string,
  title: string,
  sourceWidgetId: string,
  fieldIds?: readonly string[],
): WidgetInstance => ({
  id,
  type: "form.dynamicFieldGroup",
  title,
  props: {
    fields: sourceFields(sourceWidgetId, fieldIds),
  },
});

const sourceWidgets = (widgetIds: readonly string[]): readonly WidgetInstance[] => {
  const selectedIds = new Set(widgetIds);
  const widgets =
    basePageDefinitions
      .find((page) => page.id === "example-all-widgets")
      ?.regions.flatMap((region) => region.widgets) ?? [];

  return widgets.filter((widget) => selectedIds.has(widget.id));
};

const catalogPage = ({
  description,
  id,
  title,
  widgets,
}: {
  description: string;
  id: string;
  title: string;
  widgets: readonly WidgetInstance[];
}): PageDefinition => ({
  id,
  title,
  description,
  workflowTypes: ["*"],
  surfaceModes: catalogSurfaces,
  regions: [
    {
      id: `${id}-region`,
      layout: "grid",
      width: "wide",
      widgets,
    },
  ],
});

const widgetCatalogItem = (
  id: string,
  type: string,
  title: string,
  props: Readonly<Record<string, unknown>>,
): WidgetInstance => ({
  id,
  type,
  title,
  props,
});

type WidgetCatalogEntry = {
  id: string;
  type: string;
  title: string;
  props: Readonly<Record<string, unknown>>;
};

const widgetCatalogItems = (
  entries: readonly WidgetCatalogEntry[],
): readonly WidgetInstance[] =>
  entries.map((entry) =>
    widgetCatalogItem(entry.id, entry.type, entry.title, entry.props),
  );

const metricItems = [
  { label: "Current", value: 72, status: "info" },
  { label: "Target", value: 85, status: "success" },
  { label: "Risk", value: "Medium", status: "warning" },
] as const;

const workflowSteps = [
  { label: "Submitted", status: "success", detail: "Manager request received" },
  { label: "Review", status: "warning", detail: "Waiting HRBP approval" },
  { label: "Execute", status: "info", detail: "Pending downstream write" },
] as const;

const samplePoints = [
  { label: "Mon", value: 8 },
  { label: "Tue", value: 14 },
  { label: "Wed", value: 11 },
  { label: "Thu", value: 19 },
  { label: "Fri", value: 16 },
] as const;

const timeZones = [
  { label: "New York", offsetMinutes: -240, detail: "HRBP" },
  { label: "London", offsetMinutes: 60, detail: "Finance" },
  { label: "Singapore", offsetMinutes: 480, detail: "Payroll" },
] as const;

const typedControlPages: readonly PageDefinition[] = [
  catalogPage({
    id: "controls-basic-inputs",
    title: "Basic Input Controls",
    description:
      "Primitive generated controls for text, numeric, date, time, and contact values.",
    widgets: [
      fieldCatalogWidget(
        "primitive-input-type-controls",
        "Primitive text and numeric inputs",
        "basic-form-controls",
        ["text", "textarea", "number", "money", "percent"],
      ),
      fieldCatalogWidget(
        "bounded-date-time-type-controls",
        "Date and time bounds",
        "basic-form-controls",
        ["date", "effectiveDate", "dateRange", "time"],
      ),
      fieldCatalogWidget(
        "format-pattern-type-controls",
        "Format and pattern enforcement",
        "basic-form-controls",
        [
          "email",
          "phone",
          "url",
          "limitedText",
          "employeeCode",
          "workflowSlug",
          "lastFour",
          "postalCode",
        ],
      ),
      fieldCatalogWidget(
        "locked-system-type-controls",
        "Locked and generated values",
        "basic-form-controls",
        ["systemRequestId", "sourceSystem"],
      ),
    ],
  }),
  catalogPage({
    id: "controls-choice-selection",
    title: "Choice And Selection Controls",
    description:
      "Selection controls for dropdowns, filterable pickers, grouped options, radio sets, and checkboxes.",
    widgets: [
      fieldCatalogWidget(
        "choice-selection-type-controls",
        "Dropdowns, radios, multi-select, and checkbox controls",
        "choice-form-controls",
        [
          "select",
          "dropdown",
          "groupedDropdown",
          "filterableDropdown",
          "multiSelect",
          "radio",
          "radioGroup",
          "checkbox",
        ],
      ),
    ],
  }),
  catalogPage({
    id: "controls-toggle-slider",
    title: "Toggle And Slider Controls",
    description:
      "Binary and continuous generated controls for settings, routing switches, thresholds, and scoring.",
    widgets: [
      fieldCatalogWidget(
        "toggle-slider-type-controls",
        "Toggles and sliders",
        "choice-form-controls",
        ["toggle", "toggleGroup", "slider", "sliderGroup"],
      ),
    ],
  }),
  catalogPage({
    id: "controls-structured-groups",
    title: "Structured Grouping Controls",
    description:
      "Generated controls for repeatable values, editable tables, matrices, and drag/drop clustering.",
    widgets: [
      fieldCatalogWidget(
        "structured-grouping-type-controls",
        "Lists, tables, matrices, and clustering",
        "choice-form-controls",
        ["repeatingList", "tableEditor", "matrix", "clusterBoard"],
      ),
    ],
  }),
  catalogPage({
    id: "controls-files-signatures",
    title: "File And Signature Controls",
    description:
      "Upload, acknowledgement, attestation, and signature capture controls for workflow evidence.",
    widgets: [
      fieldCatalogWidget(
        "file-signature-type-controls",
        "Files, acknowledgements, and signatures",
        "choice-form-controls",
        ["file", "evidence", "policyAck", "signature", "signatureCapture"],
      ),
    ],
  }),
  catalogPage({
    id: "controls-hcm-domain",
    title: "HCM Domain Controls",
    description:
      "Domain-specific pickers and editors for employees, positions, org data, pay, roles, and effective-dated facts.",
    widgets: [
      fieldCatalogWidget(
        "hcm-domain-type-controls",
        "HCM pickers and workflow editors",
        "domain-form-controls",
      ),
    ],
  }),
  catalogPage({
    id: "controls-entity-access",
    title: "Entity Access Controls",
    description:
      "Permission-aware entity selection and manager-tree controls for scoped HCM workflows.",
    widgets: [
      fieldCatalogWidget(
        "entity-access-type-controls",
        "RBAC-scoped entity and tree pickers",
        "entity-access-controls",
      ),
    ],
  }),
  catalogPage({
    id: "controls-effective-change",
    title: "Effective Change Controls",
    description:
      "Effective dating and before/after editors for current, proposed, retroactive, and correction flows.",
    widgets: [
      fieldCatalogWidget(
        "effective-change-type-controls",
        "Effective dating and before/after changes",
        "effective-change-controls",
      ),
    ],
  }),
  catalogPage({
    id: "controls-comp-schedule",
    title: "Compensation And Schedule Controls",
    description:
      "Compensation package and working-time controls for pay, FTE, schedule, and time-zone workflows.",
    widgets: [
      fieldCatalogWidget(
        "comp-schedule-type-controls",
        "Compensation packages and schedule timing",
        "compensation-schedule-controls",
      ),
    ],
  }),
  catalogPage({
    id: "controls-approval-policy-ai",
    title: "Approval Policy AI Controls",
    description:
      "Approval chain, evidence checklist, and permission-aware AI review controls for governed decisions.",
    widgets: [
      fieldCatalogWidget(
        "approval-policy-ai-type-controls",
        "Approval, policy, evidence, and AI review",
        "approval-policy-controls",
      ),
    ],
  }),
  catalogPage({
    id: "controls-bulk-repair",
    title: "Bulk And Repair Controls",
    description:
      "Bulk edit, conflict resolution, and integration repair controls for exception-heavy workflows.",
    widgets: [
      fieldCatalogWidget(
        "bulk-repair-type-controls",
        "Bulk grids, conflicts, and integration repair",
        "bulk-repair-controls",
      ),
    ],
  }),
  catalogPage({
    id: "controls-privacy-regional-simulation",
    title: "Privacy Regional Simulation Controls",
    description:
      "Sensitive reveal, international contact, and transaction simulation controls for enterprise review.",
    widgets: [
      fieldCatalogWidget(
        "privacy-region-simulation-type-controls",
        "Sensitive data, regional contact, and simulation review",
        "privacy-region-simulation-controls",
      ),
    ],
  }),
];

const typedItemPages: readonly PageDefinition[] = [
  catalogPage({
    id: "items-content",
    title: "Content Items",
    description:
      "Benign display items for authored copy, markdown, HTML, links, FAQs, and label/value context.",
    widgets: sourceWidgets([
      "widget-callout",
      "widget-text",
      "widget-markdown",
      "widget-html",
      "widget-link-list",
      "widget-label-values",
      "widget-faq",
    ]),
  }),
  catalogPage({
    id: "items-data-display",
    title: "Data Display Items",
    description:
      "Tables, matrices, progress indicators, metric tiles, graphs, and drag/drop data grouping widgets.",
    widgets: sourceWidgets([
      "widget-metric",
      "widget-progress",
      "widget-metric-graph",
      "widget-graph-chart",
      "widget-node-graph",
      "widget-org-chart",
      "widget-table",
      "widget-filterable-table",
      "widget-matrix",
      "widget-cluster-board",
    ]),
  }),
  catalogPage({
    id: "items-workflow-governed",
    title: "Governed Workflow Items",
    description:
      "Workflow-authoritative widgets for queues, employee context, forms, diffs, approvals, simulations, audit, and AI review.",
    widgets: sourceWidgets([
      "widget-request-list",
      "widget-employee",
      "widget-fields",
      "widget-diff",
      "widget-approval",
      "widget-simulation",
      "widget-timeline",
      "widget-ai",
    ]),
  }),
  catalogPage({
    id: "items-media",
    title: "Media Items",
    description:
      "Image, audio, video, and PDF viewer items for customer-authored or workflow-attached media.",
    widgets: sourceWidgets([
      "widget-image",
      "widget-audio",
      "widget-video",
      "widget-pdf",
    ]),
  }),
];

const expandedWidgetPages: readonly PageDefinition[] = [
  catalogPage({
    id: "widgets-hcm-insights",
    title: "HCM Insight Widgets",
    description:
      "Niche HCM widgets for compensation, payroll, benefits, compliance, training, authorization, and org impact.",
    widgets: [
      widgetCatalogItem(
        "hcm-comp-band",
        "hcm.compensationBandVisualizer",
        "Compensation band visualizer",
        {
          body: "Current and proposed pay against the active min/mid/max range.",
          metrics: [
            { label: "Min", value: "$92k" },
            { label: "Mid", value: "$108k" },
            { label: "Max", value: "$126k" },
          ],
          percent: 64,
        },
      ),
      widgetCatalogItem("hcm-compa-ratio", "hcm.compaRatio", "Compa-ratio widget", {
        body: "Proposed base pay is 1.04x midpoint.",
        metrics: [
          { label: "Compa-ratio", value: "1.04" },
          { label: "Band", value: "Nurse 4" },
        ],
        percent: 74,
      }),
      widgetCatalogItem(
        "hcm-pay-equity",
        "hcm.payEquityScatter",
        "Pay equity comparison scatter",
        {
          body: "Peer comparison by pay, tenure, and role.",
          points: samplePoints,
        },
      ),
      widgetCatalogItem(
        "hcm-payroll-cutoff",
        "hcm.payrollCutoffCalendar",
        "Payroll cutoff calendar",
        {
          body: "Highlights cutoff windows and retro-sensitive effective dates.",
          items: [
            { label: "May 31", status: "warning", detail: "Close cutoff" },
            { label: "June 15", status: "success", detail: "Preferred cycle" },
          ],
        },
      ),
      widgetCatalogItem("hcm-retro-pay", "hcm.retroPayPreview", "Retro pay preview", {
        body: "Estimated retro pay before execution.",
        metrics: [
          { label: "Retro gross", value: "$1,184" },
          { label: "Periods", value: 2 },
        ],
      }),
      widgetCatalogItem(
        "hcm-benefits",
        "hcm.benefitsEligibilityImpact",
        "Benefits eligibility impact",
        {
          body: "Eligibility changes from FTE, job, and location changes.",
          items: [
            { label: "Medical", status: "success", detail: "No change" },
            { label: "Retirement", status: "info", detail: "Match unchanged" },
          ],
        },
      ),
      widgetCatalogItem(
        "hcm-job-impact",
        "hcm.jobProfileImpact",
        "Job profile / position impact card",
        {
          body: "Position, job profile, pay band, and worker type impact.",
          metrics: [
            { label: "Position", value: "RN-1904" },
            { label: "Pay band", value: "Nurse 4" },
          ],
        },
      ),
      widgetCatalogItem("hcm-span", "hcm.spanOfControl", "Span-of-control chart", {
        body: "Manager reporting load before and after the move.",
        metrics: [
          { label: "Before", value: 12 },
          { label: "After", value: 15 },
        ],
        percent: 75,
      }),
      widgetCatalogItem(
        "hcm-reporting-diff",
        "hcm.reportingLineDiff",
        "Reporting-line diff widget",
        {
          before: "Alex Manager -> Somerville Nursing",
          after: "Jordan Lee -> Cambridge Nursing",
        },
      ),
      widgetCatalogItem(
        "hcm-access-delta",
        "hcm.accessRoleDelta",
        "Access/role delta widget",
        {
          items: [
            { label: "Remove Somerville roster", status: "warning" },
            { label: "Add Cambridge med admin", status: "success" },
          ],
        },
      ),
      widgetCatalogItem(
        "hcm-license-expiry",
        "hcm.licenseCertificationExpiry",
        "License/certification expiry widget",
        {
          items: [
            { label: "RN License", status: "success", detail: "Expires 2027-03-01" },
            { label: "BLS", status: "warning", detail: "Expires in 32 days" },
          ],
        },
      ),
      widgetCatalogItem(
        "hcm-training-matrix",
        "hcm.trainingCompletionMatrix",
        "Training completion matrix",
        {
          rows: [
            { field: "HIPAA", current: "Complete", proposed: "Complete" },
            {
              field: "Cambridge orientation",
              current: "Missing",
              proposed: "Required",
            },
          ],
        },
      ),
      widgetCatalogItem(
        "hcm-leave-balance",
        "hcm.leaveBalanceImpact",
        "Leave balance / accrual impact",
        {
          metrics: [
            { label: "PTO", value: "84h" },
            { label: "Sick", value: "32h" },
          ],
        },
      ),
      widgetCatalogItem(
        "hcm-work-auth",
        "hcm.workAuthorizationStatus",
        "I-9 / work authorization status",
        {
          body: "Work authorization remains verified for the proposed location.",
          items: [
            { label: "I-9", status: "success" },
            { label: "E-Verify", status: "success" },
          ],
        },
      ),
      widgetCatalogItem(
        "hcm-union-contract",
        "hcm.unionContractImpact",
        "Union / contract rule impact",
        {
          body: "Contract checks for location and role.",
          items: [
            { label: "Seniority rule", status: "info" },
            { label: "Shift premium", status: "warning" },
          ],
        },
      ),
      widgetCatalogItem(
        "hcm-location-compliance",
        "hcm.locationCompliance",
        "Location / legal entity compliance",
        {
          body: "Legal entity, tax, and location compliance state.",
          items: [
            { label: "HarborCare US", status: "success" },
            { label: "MA payroll", status: "success" },
          ],
        },
      ),
    ],
  }),
  catalogPage({
    id: "widgets-workflow-ops",
    title: "Workflow Operations Widgets",
    description:
      "Operational workflow widgets for SLA, routing, approvals, dependencies, repair, state, history, and policy findings.",
    widgets: [
      widgetCatalogItem(
        "workflow-sla",
        "workflow.slaCountdown",
        "SLA countdown widget",
        {
          body: "18h remaining before compensation escalation.",
          percent: 62,
        },
      ),
      widgetCatalogItem(
        "workflow-escalation",
        "workflow.escalationPath",
        "Escalation path widget",
        { steps: workflowSteps },
      ),
      widgetCatalogItem(
        "workflow-swimlane",
        "workflow.approvalSwimlane",
        "Approval chain swimlane",
        { steps: workflowSteps },
      ),
      widgetCatalogItem(
        "workflow-parallel",
        "workflow.parallelApprovalBoard",
        "Parallel approval status board",
        { items: workflowSteps },
      ),
      widgetCatalogItem(
        "workflow-evidence",
        "workflow.requiredEvidenceChecklist",
        "Required evidence checklist widget",
        {
          items: [
            { label: "Manager rationale", status: "success" },
            { label: "Comp band support", status: "warning" },
            { label: "Payroll approval", status: "info" },
          ],
        },
      ),
      widgetCatalogItem(
        "workflow-blockers",
        "workflow.blockingIssuesPanel",
        "Blocking issues panel",
        {
          items: [
            { label: "Missing payroll watcher", status: "warning" },
            { label: "No policy block", status: "success" },
          ],
        },
      ),
      widgetCatalogItem(
        "workflow-dependencies",
        "workflow.dependencyGraph",
        "Dependency graph",
        {
          nodes: [
            { id: "request", label: "Request", x: 16, y: 50 },
            { id: "policy", label: "Policy", x: 42, y: 30 },
            { id: "payroll", label: "Payroll", x: 72, y: 58 },
          ],
          edges: [
            { source: "request", target: "policy" },
            { source: "policy", target: "payroll" },
          ],
        },
      ),
      widgetCatalogItem(
        "workflow-repair",
        "workflow.repairQueue",
        "Retry / repair queue widget",
        {
          items: [
            { label: "Workday write", status: "warning" },
            { label: "Payroll sync", status: "info" },
          ],
        },
      ),
      widgetCatalogItem(
        "workflow-state-machine",
        "workflow.stateMachineViewer",
        "State machine viewer",
        { steps: workflowSteps },
      ),
      widgetCatalogItem(
        "workflow-transition-history",
        "workflow.transitionHistory",
        "Transition history widget",
        { items: workflowSteps },
      ),
      widgetCatalogItem(
        "workflow-changed",
        "workflow.changedSinceLastReview",
        "What changed since last review",
        {
          before: "Effective date 2026-06-01",
          after: "Effective date 2026-06-15",
        },
      ),
      widgetCatalogItem(
        "workflow-handoff",
        "workflow.taskHandoff",
        "Task handoff widget",
        {
          items: [{ label: "Riley HRBP -> Priya Finance", status: "info" }],
        },
      ),
      widgetCatalogItem(
        "workflow-exception",
        "workflow.exceptionQueue",
        "Exception queue widget",
        {
          items: [
            { label: "Pay band exception", status: "warning" },
            { label: "Retro timing", status: "error" },
          ],
        },
      ),
      widgetCatalogItem(
        "workflow-policy",
        "workflow.policyFindings",
        "Policy findings panel",
        {
          items: [
            { label: "Inside band", status: "success" },
            { label: "Cutoff warning", status: "warning" },
          ],
        },
      ),
    ],
  }),
  catalogPage({
    id: "widgets-recruiting",
    title: "Recruiting And ATS Widgets",
    description:
      "Candidate, requisition, interview, offer, background-check, and recruiting communications widgets.",
    widgets: widgetCatalogItems([
      {
        id: "recruiting-pipeline",
        type: "recruiting.candidatePipelineBoard",
        title: "Candidate pipeline board",
        props: {
          items: [
            { label: "Applied", detail: "42 candidates", status: "info" },
            { label: "Interview", detail: "8 active", status: "warning" },
            { label: "Offer", detail: "2 pending", status: "success" },
          ],
        },
      },
      {
        id: "recruiting-requisition-health",
        type: "recruiting.requisitionHealthCard",
        title: "Requisition health card",
        props: {
          body: "Req is open 21 days with two finalist candidates.",
          metrics: [
            { label: "Open days", value: 21 },
            { label: "Applicants", value: 42 },
            { label: "Target start", value: "Jun 17" },
          ],
          percent: 68,
        },
      },
      {
        id: "recruiting-interview-schedule",
        type: "recruiting.interviewSchedulePanel",
        title: "Interview schedule panel",
        props: {
          items: [
            { label: "Hiring manager", detail: "Today 1:00 PM", status: "info" },
            { label: "Panel", detail: "Tomorrow 10:30 AM", status: "warning" },
          ],
        },
      },
      {
        id: "recruiting-scorecard",
        type: "recruiting.interviewScorecardSummary",
        title: "Interview scorecard summary",
        props: {
          rows: [
            { field: "Clinical skills", current: "4.7", proposed: "Strong hire" },
            { field: "Culture add", current: "4.2", proposed: "Proceed" },
          ],
        },
      },
      {
        id: "recruiting-offer-comparison",
        type: "recruiting.offerPackageComparison",
        title: "Offer package comparison",
        props: { before: "$112k base + standard bonus", after: "$118k base + sign-on" },
      },
      {
        id: "recruiting-background-check",
        type: "recruiting.backgroundCheckStatus",
        title: "Background check status",
        props: {
          items: [
            { label: "Identity", status: "success" },
            { label: "License", status: "success" },
            { label: "Criminal", status: "warning" },
          ],
        },
      },
      {
        id: "recruiting-candidate-comms",
        type: "recruiting.candidateCommunicationsTimeline",
        title: "Candidate communications timeline",
        props: {
          messages: [
            { actor: "Recruiter", body: "Sent finalist packet." },
            { actor: "Candidate", body: "Confirmed panel availability." },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-onboarding",
    title: "Onboarding Widgets",
    description:
      "New-hire journey, task, provisioning, schedule, document, and buddy assignment widgets.",
    widgets: widgetCatalogItems([
      {
        id: "onboarding-journey",
        type: "onboarding.newHireJourneyTracker",
        title: "New-hire journey tracker",
        props: { steps: workflowSteps, percent: 54 },
      },
      {
        id: "onboarding-task-bundle",
        type: "onboarding.taskBundle",
        title: "Onboarding task bundle",
        props: {
          items: [
            { label: "Complete tax forms", status: "success" },
            { label: "Review handbook", status: "warning" },
            { label: "Enroll benefits", status: "info" },
          ],
        },
      },
      {
        id: "onboarding-provisioning",
        type: "onboarding.provisioningChecklist",
        title: "Equipment / access provisioning checklist",
        props: {
          items: [
            { label: "Laptop shipped", status: "success" },
            { label: "Badge pending", status: "warning" },
            { label: "EHR access", status: "info" },
          ],
        },
      },
      {
        id: "onboarding-first-week",
        type: "onboarding.firstWeekSchedule",
        title: "First-week schedule",
        props: {
          items: [
            { label: "Day 1", detail: "Orientation" },
            { label: "Day 2", detail: "Systems training" },
            { label: "Day 3", detail: "Manager shadowing" },
          ],
        },
      },
      {
        id: "onboarding-documents",
        type: "onboarding.requiredDocuments",
        title: "Required document completion",
        props: {
          metrics: [
            { label: "Complete", value: 7 },
            { label: "Missing", value: 2 },
          ],
          percent: 78,
        },
      },
      {
        id: "onboarding-buddy",
        type: "onboarding.buddyMentorAssignment",
        title: "Buddy / mentor assignment",
        props: {
          items: [{ label: "Maya Chen", detail: "Clinical mentor", status: "success" }],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-offboarding",
    title: "Offboarding Widgets",
    description:
      "Termination, exit, knowledge-transfer, asset, access, final pay, and severance widgets.",
    widgets: widgetCatalogItems([
      {
        id: "offboarding-termination",
        type: "offboarding.terminationChecklist",
        title: "Termination checklist",
        props: {
          items: [
            { label: "Manager approval", status: "success" },
            { label: "Legal review", status: "warning" },
            { label: "Final day notice", status: "info" },
          ],
        },
      },
      {
        id: "offboarding-exit-interview",
        type: "offboarding.exitInterviewSummary",
        title: "Exit interview summary",
        props: { body: "Voluntary exit. Primary reason: career growth." },
      },
      {
        id: "offboarding-knowledge-transfer",
        type: "offboarding.knowledgeTransferTracker",
        title: "Knowledge transfer tracker",
        props: { steps: workflowSteps, percent: 66 },
      },
      {
        id: "offboarding-asset-return",
        type: "offboarding.assetReturn",
        title: "Asset return widget",
        props: {
          items: [
            { label: "Laptop", status: "warning" },
            { label: "Badge", status: "success" },
            { label: "Mobile device", status: "info" },
          ],
        },
      },
      {
        id: "offboarding-access-revocation",
        type: "offboarding.accessRevocationStatus",
        title: "Access revocation status",
        props: {
          items: [
            { label: "Workday", status: "success" },
            { label: "EHR", status: "warning" },
            { label: "Payroll", status: "info" },
          ],
        },
      },
      {
        id: "offboarding-final-pay",
        type: "offboarding.finalPaySeveranceChecklist",
        title: "Final pay / severance checklist",
        props: {
          rows: [
            { field: "Final pay", current: "Calculated", proposed: "$4,280" },
            { field: "Severance", current: "Eligible", proposed: "2 weeks" },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-performance",
    title: "Performance Widgets",
    description:
      "Goals, review cycle, rating, calibration, feedback, improvement plan, and promotion readiness widgets.",
    widgets: widgetCatalogItems([
      {
        id: "performance-goals",
        type: "performance.goalsOkrTracker",
        title: "Goals / OKR tracker",
        props: { items: workflowSteps, percent: 72 },
      },
      {
        id: "performance-review-cycle",
        type: "performance.reviewCycleStatus",
        title: "Review cycle status",
        props: {
          items: [
            { label: "Self review", status: "success" },
            { label: "Manager review", status: "warning" },
            { label: "Calibration", status: "info" },
          ],
        },
      },
      {
        id: "performance-rating-distribution",
        type: "performance.ratingDistribution",
        title: "Rating distribution",
        props: { points: samplePoints },
      },
      {
        id: "performance-calibration",
        type: "performance.calibrationMatrix",
        title: "Calibration matrix",
        props: {
          rows: [
            { field: "Exceeds", current: "14%", proposed: "16%" },
            { field: "Meets", current: "72%", proposed: "70%" },
          ],
        },
      },
      {
        id: "performance-feedback",
        type: "performance.feedbackTimeline",
        title: "Feedback timeline",
        props: { items: workflowSteps },
      },
      {
        id: "performance-pip",
        type: "performance.improvementPlanTracker",
        title: "Performance improvement plan tracker",
        props: {
          items: [
            { label: "Plan issued", status: "success" },
            { label: "30-day check", status: "warning" },
          ],
        },
      },
      {
        id: "performance-promotion-readiness",
        type: "performance.promotionReadinessCard",
        title: "Promotion readiness card",
        props: {
          body: "Ready in 1-2 cycles with scope expansion.",
          metrics: metricItems,
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-talent",
    title: "Talent And Succession Widgets",
    description:
      "9-box, succession bench, skills, competency, mobility, talent pool, and retention-risk widgets.",
    widgets: widgetCatalogItems([
      {
        id: "talent-nine-box",
        type: "talent.nineBoxGrid",
        title: "9-box talent grid",
        props: {
          rows: [
            { field: "High potential", current: "8", proposed: "Ready now" },
            { field: "Core talent", current: "42", proposed: "Develop" },
          ],
        },
      },
      {
        id: "talent-succession-bench",
        type: "talent.successionBench",
        title: "Succession bench widget",
        props: {
          items: [
            { label: "Priya Shah", detail: "Ready now", status: "success" },
            { label: "Owen Diaz", detail: "1-2 years", status: "info" },
          ],
        },
      },
      {
        id: "talent-skills",
        type: "talent.skillsInventory",
        title: "Skills inventory",
        props: {
          items: [
            { label: "Critical care", status: "success" },
            { label: "Charge nurse", status: "warning" },
          ],
        },
      },
      {
        id: "talent-competency",
        type: "talent.competencyHeatmap",
        title: "Competency heatmap",
        props: { points: samplePoints },
      },
      {
        id: "talent-mobility",
        type: "talent.internalMobilityMatch",
        title: "Internal mobility match card",
        props: { body: "84% match to Clinical Lead role.", percent: 84 },
      },
      {
        id: "talent-pool",
        type: "talent.talentPoolRoster",
        title: "Talent pool roster",
        props: {
          items: [
            { label: "Future managers", detail: "14 employees" },
            { label: "Critical skills", detail: "9 employees" },
          ],
        },
      },
      {
        id: "talent-retention-risk",
        type: "talent.flightRetentionRisk",
        title: "Flight-risk / retention risk widget",
        props: {
          body: "Medium risk based on comp position and manager change.",
          metrics: metricItems,
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-workforce-planning",
    title: "Workforce Planning Widgets",
    description:
      "Headcount, position control, vacancy, scenario, budget, attrition, and span/layer planning widgets.",
    widgets: widgetCatalogItems([
      {
        id: "workforce-headcount",
        type: "workforce.headcountPlanActual",
        title: "Headcount plan vs actual",
        props: { before: "Plan: 128 roles", after: "Actual: 121 filled" },
      },
      {
        id: "workforce-position-control",
        type: "workforce.positionControl",
        title: "Position control widget",
        props: {
          items: [
            { label: "Approved positions", detail: "128" },
            { label: "Open positions", detail: "7" },
          ],
        },
      },
      {
        id: "workforce-vacancy-aging",
        type: "workforce.vacancyAging",
        title: "Vacancy aging",
        props: { points: samplePoints },
      },
      {
        id: "workforce-scenario",
        type: "workforce.scenarioPlanner",
        title: "Workforce scenario planner",
        props: {
          rows: [
            { field: "Baseline", current: "121 filled", proposed: "$14.2M" },
            { field: "Growth", current: "136 filled", proposed: "$15.8M" },
          ],
        },
      },
      {
        id: "workforce-budget-filled",
        type: "workforce.budgetedFilledRoles",
        title: "Budgeted vs filled roles",
        props: { percent: 94, metrics: metricItems },
      },
      {
        id: "workforce-attrition",
        type: "workforce.attritionForecast",
        title: "Attrition forecast",
        props: { points: samplePoints, body: "Projected attrition is trending down." },
      },
      {
        id: "workforce-span-layer",
        type: "workforce.spanLayerAnalysis",
        title: "Span/layer analysis",
        props: {
          metrics: [
            { label: "Avg span", value: 7.4 },
            { label: "Layers", value: 5 },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-scheduling-attendance",
    title: "Scheduling And Attendance Widgets",
    description:
      "Shift, timecard, overtime, missed-punch, attendance, coverage, and leave-calendar widgets.",
    widgets: widgetCatalogItems([
      {
        id: "scheduling-shift-grid",
        type: "scheduling.shiftScheduleGrid",
        title: "Shift schedule grid",
        props: {
          rows: [
            { field: "Monday", current: "7a-3p", proposed: "Covered" },
            { field: "Tuesday", current: "3p-11p", proposed: "Gap" },
          ],
        },
      },
      {
        id: "scheduling-timecard-exception",
        type: "scheduling.timecardExceptionPanel",
        title: "Timecard exception panel",
        props: {
          items: [
            { label: "Meal break missing", status: "warning" },
            { label: "Unapproved OT", status: "error" },
          ],
        },
      },
      {
        id: "scheduling-overtime-risk",
        type: "scheduling.overtimeRisk",
        title: "Overtime risk widget",
        props: {
          body: "Projected overtime risk is high for weekend coverage.",
          percent: 88,
        },
      },
      {
        id: "scheduling-missed-punch",
        type: "scheduling.missedPunchQueue",
        title: "Missed punch queue",
        props: {
          items: [
            { label: "Jane Rivera", detail: "Clock-out missing", status: "warning" },
          ],
        },
      },
      {
        id: "scheduling-attendance-pattern",
        type: "scheduling.attendancePatternDetector",
        title: "Attendance pattern detector",
        props: {
          points: samplePoints,
          body: "Three late arrivals detected in 30 days.",
        },
      },
      {
        id: "scheduling-coverage-gap",
        type: "scheduling.coverageGapHeatmap",
        title: "Coverage gap heatmap",
        props: { points: samplePoints },
      },
      {
        id: "scheduling-leave-overlay",
        type: "scheduling.leaveCalendarOverlay",
        title: "Leave calendar overlay",
        props: {
          items: [
            { label: "PTO", detail: "3 employees" },
            { label: "LOA", detail: "1 employee" },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-leave-absence",
    title: "Leave And Absence Widgets",
    description:
      "Leave request, FMLA/LOA, intermittent usage, return-to-work, trends, accrual, and conflict widgets.",
    widgets: widgetCatalogItems([
      {
        id: "leave-status",
        type: "leave.requestStatus",
        title: "Leave request status",
        props: { steps: workflowSteps },
      },
      {
        id: "leave-fmla-loa",
        type: "leave.fmlaLoaCaseTracker",
        title: "FMLA / LOA case tracker",
        props: {
          items: [
            { label: "Eligibility", status: "success" },
            { label: "Certification", status: "warning" },
          ],
        },
      },
      {
        id: "leave-intermittent",
        type: "leave.intermittentUsage",
        title: "Intermittent leave usage",
        props: { points: samplePoints },
      },
      {
        id: "leave-return-to-work",
        type: "leave.returnToWorkChecklist",
        title: "Return-to-work checklist",
        props: {
          items: [
            { label: "Medical release", status: "warning" },
            { label: "Schedule confirmed", status: "info" },
          ],
        },
      },
      {
        id: "leave-absence-trend",
        type: "leave.absenceTrend",
        title: "Absence trend widget",
        props: { points: samplePoints },
      },
      {
        id: "leave-accrual-forecast",
        type: "leave.accrualForecast",
        title: "Accrual forecast",
        props: {
          metrics: [
            { label: "Current PTO", value: "84h" },
            { label: "Forecast", value: "102h" },
          ],
        },
      },
      {
        id: "leave-conflict",
        type: "leave.conflictDetector",
        title: "Leave conflict detector",
        props: {
          items: [
            { label: "Coverage conflict", status: "warning" },
            { label: "Blackout policy", status: "success" },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-benefits",
    title: "Benefits Widgets",
    description:
      "Enrollment, life event, dependent verification, open enrollment, cost, and COBRA widgets.",
    widgets: widgetCatalogItems([
      {
        id: "benefits-enrollment",
        type: "benefits.enrollmentProgress",
        title: "Benefits enrollment progress",
        props: { percent: 62, metrics: metricItems },
      },
      {
        id: "benefits-life-event",
        type: "benefits.lifeEventImpact",
        title: "Life-event impact widget",
        props: { before: "Employee only", after: "Employee + family" },
      },
      {
        id: "benefits-dependent-verification",
        type: "benefits.dependentVerificationStatus",
        title: "Dependent verification status",
        props: {
          items: [
            { label: "Spouse", status: "success" },
            { label: "Child", status: "warning" },
          ],
        },
      },
      {
        id: "benefits-open-enrollment",
        type: "benefits.openEnrollmentDecisionSupport",
        title: "Open enrollment decision support",
        props: {
          body: "Recommended plan based on utilization and payroll deductions.",
        },
      },
      {
        id: "benefits-cost",
        type: "benefits.costComparison",
        title: "Benefits cost comparison",
        props: {
          rows: [
            { field: "Medical", current: "$242", proposed: "$318" },
            { field: "Dental", current: "$28", proposed: "$31" },
          ],
        },
      },
      {
        id: "benefits-cobra",
        type: "benefits.cobraEligibilityTracker",
        title: "COBRA eligibility tracker",
        props: {
          items: [
            { label: "Eligible", detail: "Notice due in 14 days", status: "warning" },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-payroll-tax",
    title: "Payroll And Tax Widgets",
    description:
      "Gross-to-net, pay statement, payroll exception, tax, deduction, retro, and reconciliation widgets.",
    widgets: widgetCatalogItems([
      {
        id: "payroll-gross-net",
        type: "payroll.grossToNetPreview",
        title: "Gross-to-net preview",
        props: {
          metrics: [
            { label: "Gross", value: "$4,820" },
            { label: "Net", value: "$3,418" },
          ],
        },
      },
      {
        id: "payroll-statement",
        type: "payroll.payStatementPreview",
        title: "Pay statement preview",
        props: { body: "Branded pay statement preview with earnings and deductions." },
      },
      {
        id: "payroll-exceptions",
        type: "payroll.exceptionQueue",
        title: "Payroll exception queue",
        props: {
          items: [
            { label: "Tax code missing", status: "error" },
            { label: "Retro calc pending", status: "warning" },
          ],
        },
      },
      {
        id: "payroll-tax-impact",
        type: "payroll.taxJurisdictionImpact",
        title: "Tax jurisdiction impact",
        props: { before: "MA resident", after: "MA resident + NY work state" },
      },
      {
        id: "payroll-deductions",
        type: "payroll.garnishmentDeductionSummary",
        title: "Garnishment / deduction summary",
        props: {
          rows: [
            { field: "401k", current: "6%", proposed: "$289" },
            { field: "Garnishment", current: "Active", proposed: "$120" },
          ],
        },
      },
      {
        id: "payroll-retro-detail",
        type: "payroll.retroPayDetailBreakdown",
        title: "Retro pay detail breakdown",
        props: {
          rows: [
            { field: "Period 1", current: "$420", proposed: "$510" },
            { field: "Period 2", current: "$420", proposed: "$674" },
          ],
        },
      },
      {
        id: "payroll-reconciliation",
        type: "payroll.reconciliation",
        title: "Payroll reconciliation widget",
        props: {
          percent: 96,
          metrics: [
            { label: "Matched", value: 126 },
            { label: "Diffs", value: 5 },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-employee-relations",
    title: "Employee Relations Widgets",
    description:
      "HR case, investigation, grievance, accommodation, discipline, policy violation, and confidential note widgets.",
    widgets: widgetCatalogItems([
      {
        id: "er-case-status",
        type: "employeeRelations.caseStatus",
        title: "HR case status",
        props: { steps: workflowSteps },
      },
      {
        id: "er-investigation",
        type: "employeeRelations.investigationTimeline",
        title: "Investigation timeline",
        props: { items: workflowSteps },
      },
      {
        id: "er-grievance",
        type: "employeeRelations.grievanceTracker",
        title: "Grievance tracker",
        props: {
          items: [
            { label: "Filed", status: "success" },
            { label: "Review", status: "warning" },
          ],
        },
      },
      {
        id: "er-accommodation",
        type: "employeeRelations.accommodationRequestTracker",
        title: "Accommodation request tracker",
        props: { body: "Interactive ADA accommodation request status and evidence." },
      },
      {
        id: "er-disciplinary-history",
        type: "employeeRelations.disciplinaryActionHistory",
        title: "Disciplinary action history",
        props: { items: workflowSteps },
      },
      {
        id: "er-policy-violation",
        type: "employeeRelations.policyViolationSummary",
        title: "Policy violation summary",
        props: {
          rows: [
            { field: "Policy", current: "Attendance", proposed: "2nd notice" },
            { field: "Risk", current: "Medium", proposed: "HR review" },
          ],
        },
      },
      {
        id: "er-confidential-notes",
        type: "employeeRelations.confidentialNotesPanel",
        title: "Confidential notes panel",
        props: { body: "Restricted ER notes, scoped by role and workflow state." },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-compliance-risk",
    title: "Compliance And Risk Widgets",
    description:
      "Attestation, audit readiness, regulatory deadline, protected class, retention, and disclosure widgets.",
    widgets: widgetCatalogItems([
      {
        id: "compliance-attestation",
        type: "compliance.policyAttestationTracker",
        title: "Mandatory policy attestation tracker",
        props: { percent: 87, metrics: metricItems },
      },
      {
        id: "compliance-audit-readiness",
        type: "compliance.auditReadinessScore",
        title: "Audit readiness score",
        props: { body: "Evidence packet is nearly complete.", percent: 91 },
      },
      {
        id: "compliance-deadlines",
        type: "compliance.regulatoryDeadlineTracker",
        title: "Regulatory deadline tracker",
        props: {
          items: [
            { label: "EEO-1", detail: "Due Jun 4", status: "warning" },
            { label: "ACA filing", detail: "Complete", status: "success" },
          ],
        },
      },
      {
        id: "compliance-protected-class",
        type: "compliance.protectedClassImpactSummary",
        title: "Protected class impact summary",
        props: {
          body: "No adverse impact flag for the selected population.",
          metrics: [{ label: "Variance", value: "1.8%" }],
        },
      },
      {
        id: "compliance-retention",
        type: "compliance.recordRetentionStatus",
        title: "Record retention status",
        props: {
          items: [
            { label: "Personnel file", status: "success" },
            { label: "Investigation notes", status: "warning" },
          ],
        },
      },
      {
        id: "compliance-consent",
        type: "compliance.consentDisclosureStatus",
        title: "Consent / disclosure status",
        props: {
          items: [
            { label: "Disclosure sent", status: "success" },
            { label: "Consent signed", status: "warning" },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-employee-experience",
    title: "Employee Experience Widgets",
    description:
      "Pulse survey, sentiment, eNPS, recognition, internal comms, and workload signal widgets.",
    widgets: widgetCatalogItems([
      {
        id: "experience-pulse",
        type: "experience.pulseSurveyResults",
        title: "Pulse survey results",
        props: { points: samplePoints },
      },
      {
        id: "experience-sentiment",
        type: "experience.engagementSentimentTrend",
        title: "Engagement sentiment trend",
        props: {
          points: samplePoints,
          body: "Sentiment is improving after manager change.",
        },
      },
      {
        id: "experience-enps",
        type: "experience.enps",
        title: "eNPS widget",
        props: {
          metrics: [
            { label: "eNPS", value: 42 },
            { label: "Responses", value: 314 },
          ],
        },
      },
      {
        id: "experience-recognition",
        type: "experience.recognitionWall",
        title: "Recognition wall",
        props: {
          messages: [
            { actor: "Maya", body: "Thanks for covering the late shift." },
            { actor: "Owen", body: "Great handoff during census surge." },
          ],
        },
      },
      {
        id: "experience-comms",
        type: "experience.internalCommsAnnouncement",
        title: "Internal comms announcement widget",
        props: {
          body: "Open enrollment begins Monday. Review plan guidance by Friday.",
        },
      },
      {
        id: "experience-workload",
        type: "experience.burnoutWorkloadSignal",
        title: "Burnout / workload signal",
        props: {
          body: "Workload signal is elevated for two teams.",
          items: [
            { label: "ICU", status: "warning" },
            { label: "Emergency", status: "error" },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-core-hris",
    title: "Employee Core / HRIS Widgets",
    description:
      "Employee 360, worker data quality, employment history, personal info, contacts, payroll setup, roster, and org history widgets.",
    widgets: widgetCatalogItems([
      {
        id: "core-employee-360",
        type: "coreHris.employee360Profile",
        title: "Employee 360 profile",
        props: {
          metrics: [
            { label: "Tenure", value: "4y 7m" },
            { label: "Role", value: "RN IV" },
            { label: "Location", value: "Cambridge" },
          ],
          items: [
            { label: "Manager", detail: "Jordan Lee" },
            { label: "Cost center", detail: "Nursing / ICU" },
          ],
        },
      },
      {
        id: "core-data-completeness",
        type: "coreHris.workerDataCompletenessScore",
        title: "Worker data completeness score",
        props: { percent: 92, metrics: [{ label: "Missing fields", value: 3 }] },
      },
      {
        id: "core-employment-history",
        type: "coreHris.employmentHistoryTimeline",
        title: "Employment history timeline",
        props: { items: workflowSteps },
      },
      {
        id: "core-personal-info-change",
        type: "coreHris.personalInfoChangeSummary",
        title: "Personal info change summary",
        props: { before: "Jane A. Rivera", after: "Jane Rivera-Santos" },
      },
      {
        id: "core-emergency-contact",
        type: "coreHris.emergencyContactCard",
        title: "Emergency contact card",
        props: {
          items: [
            { label: "Luis Rivera", detail: "Spouse / primary" },
            { label: "Mara Santos", detail: "Parent / alternate" },
          ],
        },
      },
      {
        id: "core-direct-deposit",
        type: "coreHris.directDepositStatus",
        title: "Direct deposit status",
        props: {
          items: [
            { label: "Primary account", status: "success" },
            { label: "Prenote", status: "warning" },
          ],
        },
      },
      {
        id: "core-job-position-history",
        type: "coreHris.jobPositionHistory",
        title: "Job / position history",
        props: { items: workflowSteps },
      },
      {
        id: "core-manager-roster",
        type: "coreHris.managerTeamRoster",
        title: "Manager team roster",
        props: {
          items: [
            { label: "Jane Rivera", detail: "RN IV" },
            { label: "Owen Diaz", detail: "RN III" },
            { label: "Maya Chen", detail: "RN V" },
          ],
        },
      },
      {
        id: "core-cost-center-history",
        type: "coreHris.orgMembershipCostCenterHistory",
        title: "Org membership / cost center history",
        props: { before: "Somerville Nursing / 4210", after: "Cambridge ICU / 5340" },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-compensation-advanced",
    title: "Advanced Compensation Widgets",
    description:
      "Total rewards, merit budget, incentives, equity, salary history, range position, premium pay, budget burndown, and market adjustment widgets.",
    widgets: widgetCatalogItems([
      {
        id: "comp-total-rewards",
        type: "compAdvanced.totalRewardsStatement",
        title: "Total rewards statement",
        props: {
          metrics: [
            { label: "Base", value: "$118k" },
            { label: "Benefits", value: "$22k" },
            { label: "Bonus", value: "$8k" },
          ],
        },
      },
      {
        id: "comp-merit-budget",
        type: "compAdvanced.meritCycleBudgetTracker",
        title: "Merit cycle budget tracker",
        props: { percent: 73, metrics: [{ label: "Remaining", value: "$184k" }] },
      },
      {
        id: "comp-bonus-calculator",
        type: "compAdvanced.bonusIncentiveCalculator",
        title: "Bonus / incentive payout calculator",
        props: {
          rows: [
            { field: "Target", current: "8%", proposed: "$9,440" },
            { field: "Multiplier", current: "112%", proposed: "$10,573" },
          ],
        },
      },
      {
        id: "comp-equity-vesting",
        type: "compAdvanced.equityGrantVesting",
        title: "Equity grant / vesting widget",
        props: {
          items: [{ label: "RSU grant", detail: "25% vested", status: "info" }],
        },
      },
      {
        id: "comp-salary-history",
        type: "compAdvanced.salaryHistoryTimeline",
        title: "Salary history timeline",
        props: { points: samplePoints },
      },
      {
        id: "comp-range-penetration",
        type: "compAdvanced.rangePenetration",
        title: "Range penetration widget",
        props: { percent: 64, metrics: [{ label: "Compa-ratio", value: "1.04" }] },
      },
      {
        id: "comp-shift-premium",
        type: "compAdvanced.shiftDifferentialPremiumPay",
        title: "Shift differential / premium pay widget",
        props: {
          rows: [
            { field: "Evening", current: "$2.50/hr", proposed: "$320/mo" },
            { field: "Weekend", current: "$4.00/hr", proposed: "$224/mo" },
          ],
        },
      },
      {
        id: "comp-budget-burndown",
        type: "compAdvanced.compBudgetBurndown",
        title: "Comp budget burn-down",
        props: { points: samplePoints },
      },
      {
        id: "comp-market-adjustment",
        type: "compAdvanced.marketAdjustmentRecommendation",
        title: "Market adjustment recommendation",
        props: {
          body: "Recommend 3.8% market adjustment based on peer cohort.",
          percent: 82,
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-learning-development",
    title: "Learning And Development Widgets",
    description:
      "Learning plans, course catalog, skills gaps, certifications, CEUs, waitlists, coaching, and career path widgets.",
    widgets: widgetCatalogItems([
      {
        id: "learning-plan",
        type: "learning.learningPlanTracker",
        title: "Learning plan tracker",
        props: { steps: workflowSteps, percent: 58 },
      },
      {
        id: "learning-catalog",
        type: "learning.lmsCourseCatalog",
        title: "LMS course catalog widget",
        props: {
          items: [
            { label: "Charge nurse basics", detail: "2h" },
            { label: "HIPAA annual", detail: "45m" },
            { label: "Safety refresher", detail: "1h" },
          ],
        },
      },
      {
        id: "learning-skill-gap",
        type: "learning.skillGapAnalysis",
        title: "Skill gap analysis",
        props: { before: "Current skill match 68%", after: "After plan 91%" },
      },
      {
        id: "learning-cert-compliance",
        type: "learning.certificationComplianceDashboard",
        title: "Certification compliance dashboard",
        props: { percent: 88, items: [{ label: "BLS due soon", status: "warning" }] },
      },
      {
        id: "learning-ceu",
        type: "learning.continuingEducationCredits",
        title: "CEU / continuing education credits",
        props: {
          metrics: [
            { label: "Earned", value: 14 },
            { label: "Required", value: 20 },
          ],
        },
      },
      {
        id: "learning-waitlist",
        type: "learning.trainingWaitlist",
        title: "Training waitlist",
        props: {
          items: [
            { label: "Advanced telemetry", detail: "Position 4", status: "info" },
          ],
        },
      },
      {
        id: "learning-coaching-plan",
        type: "learning.managerCoachingPlan",
        title: "Manager coaching plan",
        props: { items: workflowSteps },
      },
      {
        id: "learning-career-path",
        type: "learning.careerPathVisualizer",
        title: "Career path visualizer",
        props: {
          items: [
            { label: "RN IV", status: "success" },
            { label: "Charge Nurse", status: "warning" },
            { label: "Nurse Manager", status: "info" },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-dei-equity",
    title: "DEI And Workforce Equity Widgets",
    description:
      "Representation, diversity funnel, adverse impact, pay equity, promotion equity, inclusion survey, and accommodation trend widgets.",
    widgets: widgetCatalogItems([
      {
        id: "dei-representation",
        type: "dei.representationDashboard",
        title: "Representation dashboard",
        props: { points: samplePoints },
      },
      {
        id: "dei-hiring-funnel",
        type: "dei.diversityHiringFunnel",
        title: "Diversity hiring funnel",
        props: { points: samplePoints },
      },
      {
        id: "dei-adverse-impact",
        type: "dei.adverseImpactAnalysis",
        title: "Adverse impact analysis",
        props: {
          body: "No adverse impact threshold exceeded.",
          metrics: [{ label: "Ratio", value: "0.91" }],
        },
      },
      {
        id: "dei-pay-equity",
        type: "dei.payEquityCohortExplorer",
        title: "Pay equity cohort explorer",
        props: { points: samplePoints },
      },
      {
        id: "dei-promotion-equity",
        type: "dei.promotionEquityTracker",
        title: "Promotion equity tracker",
        props: { rows: [{ field: "Women", current: "48%", proposed: "51%" }] },
      },
      {
        id: "dei-inclusion-survey",
        type: "dei.inclusionSurveyBreakdown",
        title: "Inclusion survey breakdown",
        props: {
          metrics: [
            { label: "Belonging", value: "78" },
            { label: "Trust", value: "72" },
          ],
        },
      },
      {
        id: "dei-accommodation-trend",
        type: "dei.accommodationTrendDashboard",
        title: "Accommodation trend dashboard",
        props: { points: samplePoints },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-labor-union",
    title: "Labor And Union Widgets",
    description:
      "Seniority, bargaining agreement rules, shift bids, recall rights, dues, grievances, and contract eligibility widgets.",
    widgets: widgetCatalogItems([
      {
        id: "labor-seniority",
        type: "labor.seniorityRoster",
        title: "Seniority roster",
        props: {
          items: [
            { label: "Maya Chen", detail: "18 years" },
            { label: "Jane Rivera", detail: "9 years" },
          ],
        },
      },
      {
        id: "labor-cba-rules",
        type: "labor.collectiveBargainingAgreementRuleSummary",
        title: "Collective bargaining agreement rule summary",
        props: {
          items: [
            { label: "Overtime rule", status: "warning" },
            { label: "Shift premium", status: "success" },
          ],
        },
      },
      {
        id: "labor-shift-bid",
        type: "labor.shiftBidBoard",
        title: "Shift bid board",
        props: {
          rows: [{ field: "Night shift", current: "6 bids", proposed: "Jane rank #2" }],
        },
      },
      {
        id: "labor-bumping-recall",
        type: "labor.bumpingRecallRights",
        title: "Bumping / recall rights widget",
        props: { body: "Employee has recall rights for two equivalent roles." },
      },
      {
        id: "labor-union-dues",
        type: "labor.unionDuesStatus",
        title: "Union dues status",
        props: {
          items: [
            { label: "Current", status: "success" },
            { label: "Deduction active", status: "success" },
          ],
        },
      },
      {
        id: "labor-grievance-stage",
        type: "labor.grievanceStageBoard",
        title: "Grievance stage board",
        props: { steps: workflowSteps },
      },
      {
        id: "labor-contract-eligibility",
        type: "labor.contractEligibilityChecker",
        title: "Contract eligibility checker",
        props: { items: [{ label: "Bargaining unit eligible", status: "success" }] },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-health-safety",
    title: "Health And Safety Widgets",
    description:
      "Incident, OSHA, workers' comp, return-to-duty, safety training, exposure, vaccination, and injury trend widgets.",
    widgets: widgetCatalogItems([
      {
        id: "safety-incident",
        type: "healthSafety.incidentReportTracker",
        title: "Incident report tracker",
        props: { steps: workflowSteps },
      },
      {
        id: "safety-osha",
        type: "healthSafety.oshaLogSummary",
        title: "OSHA log summary",
        props: {
          metrics: [
            { label: "Recordable", value: 3 },
            { label: "YTD rate", value: "1.2" },
          ],
        },
      },
      {
        id: "safety-workers-comp",
        type: "healthSafety.workersCompCaseTracker",
        title: "Workers' comp case tracker",
        props: {
          items: [
            { label: "Claim opened", status: "success" },
            { label: "Medical review", status: "warning" },
          ],
        },
      },
      {
        id: "safety-return-duty",
        type: "healthSafety.returnToDutyStatus",
        title: "Return-to-duty status",
        props: { items: [{ label: "Restrictions reviewed", status: "warning" }] },
      },
      {
        id: "safety-training",
        type: "healthSafety.safetyTrainingCompliance",
        title: "Safety training compliance",
        props: { percent: 86, metrics: [{ label: "Overdue", value: 12 }] },
      },
      {
        id: "safety-exposure-vaccine",
        type: "healthSafety.exposureVaccinationStatus",
        title: "Exposure / vaccination status",
        props: {
          items: [
            { label: "TB screen", status: "success" },
            { label: "Flu vaccine", status: "warning" },
          ],
        },
      },
      {
        id: "safety-injury-trend",
        type: "healthSafety.workplaceInjuryTrend",
        title: "Workplace injury trend widget",
        props: { points: samplePoints },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-hr-service-delivery",
    title: "HR Service Delivery Widgets",
    description:
      "Knowledge article, case SLA, categorization, chatbot transcript, escalation, volume forecast, and CSAT widgets.",
    widgets: widgetCatalogItems([
      {
        id: "service-kb",
        type: "serviceDelivery.knowledgeArticleViewer",
        title: "HR knowledge article viewer",
        props: { body: "Policy article preview with entitlement-aware related links." },
      },
      {
        id: "service-case-sla",
        type: "serviceDelivery.caseSlaDashboard",
        title: "Case SLA dashboard",
        props: {
          percent: 79,
          metrics: [
            { label: "At risk", value: 8 },
            { label: "Breached", value: 2 },
          ],
        },
      },
      {
        id: "service-category",
        type: "serviceDelivery.caseCategorization",
        title: "Case categorization widget",
        props: {
          items: [{ label: "Payroll", detail: "82% confidence", status: "info" }],
        },
      },
      {
        id: "service-chatbot",
        type: "serviceDelivery.chatbotTranscriptPanel",
        title: "HR chatbot transcript panel",
        props: {
          messages: [
            { actor: "Employee", body: "How do I update direct deposit?" },
            { actor: "Bot", body: "Open Payroll > Payment elections." },
          ],
        },
      },
      {
        id: "service-escalation",
        type: "serviceDelivery.escalationReasonSummary",
        title: "Escalation reason summary",
        props: {
          body: "Escalated due to payroll deadline and missing banking verification.",
        },
      },
      {
        id: "service-volume",
        type: "serviceDelivery.serviceCenterVolumeForecast",
        title: "Service center volume forecast",
        props: { points: samplePoints },
      },
      {
        id: "service-csat",
        type: "serviceDelivery.caseCsatSurvey",
        title: "CSAT after-case survey widget",
        props: {
          metrics: [
            { label: "CSAT", value: "4.6" },
            { label: "Responses", value: 128 },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-employee-finance",
    title: "Employee Finance Widgets",
    description:
      "Expense, relocation, tuition reimbursement, referral bonus, commuter benefit, and payroll advance widgets.",
    widgets: widgetCatalogItems([
      {
        id: "finance-expense",
        type: "employeeFinance.expenseApprovalSummary",
        title: "Expense approval summary",
        props: {
          rows: [{ field: "Travel", current: "$842", proposed: "Pending manager" }],
        },
      },
      {
        id: "finance-relocation",
        type: "employeeFinance.relocationPackageTracker",
        title: "Relocation package tracker",
        props: {
          percent: 46,
          items: [
            { label: "Mover booked", status: "success" },
            { label: "Temporary housing", status: "warning" },
          ],
        },
      },
      {
        id: "finance-tuition",
        type: "employeeFinance.tuitionReimbursementTracker",
        title: "Tuition reimbursement tracker",
        props: {
          items: [
            { label: "Course approved", status: "success" },
            { label: "Transcript required", status: "warning" },
          ],
        },
      },
      {
        id: "finance-referral",
        type: "employeeFinance.referralBonusTracker",
        title: "Referral bonus tracker",
        props: { before: "Candidate hired", after: "$1,500 payout after 90 days" },
      },
      {
        id: "finance-commuter",
        type: "employeeFinance.commuterBenefitUsage",
        title: "Commuter benefit usage",
        props: {
          metrics: [
            { label: "Monthly election", value: "$120" },
            { label: "Used", value: "$84" },
          ],
        },
      },
      {
        id: "finance-payroll-advance",
        type: "employeeFinance.payrollAdvanceRequestStatus",
        title: "Payroll advance request status",
        props: { steps: workflowSteps },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-global-mobility",
    title: "Global And Mobility Widgets",
    description:
      "Visa sponsorship, immigration, global assignment, expat tax, country compliance, and relocation move widgets.",
    widgets: widgetCatalogItems([
      {
        id: "global-visa",
        type: "globalMobility.visaSponsorshipTracker",
        title: "Visa sponsorship tracker",
        props: { steps: workflowSteps },
      },
      {
        id: "global-immigration",
        type: "globalMobility.immigrationMilestoneTimeline",
        title: "Immigration milestone timeline",
        props: { items: workflowSteps },
      },
      {
        id: "global-assignment",
        type: "globalMobility.globalAssignmentPackage",
        title: "Global assignment package",
        props: { rows: [{ field: "Host country", current: "US", proposed: "UK" }] },
      },
      {
        id: "global-expat-tax",
        type: "globalMobility.expatTaxEqualizationSummary",
        title: "Expat tax equalization summary",
        props: { before: "Home net pay $8,200", after: "Equalized net pay $8,180" },
      },
      {
        id: "global-country-compliance",
        type: "globalMobility.countryComplianceChecklist",
        title: "Country-specific compliance checklist",
        props: {
          items: [
            { label: "Right to work", status: "warning" },
            { label: "Local contract", status: "info" },
          ],
        },
      },
      {
        id: "global-relocation-move",
        type: "globalMobility.relocationMoveTracker",
        title: "Relocation move tracker",
        props: {
          percent: 38,
          items: [
            { label: "Shipment", status: "info" },
            { label: "Housing", status: "warning" },
          ],
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-privacy-security",
    title: "Privacy And Security Widgets",
    description:
      "Sensitive field audit, data subject request, consent expiration, legal hold, access review, and privacy assessment widgets.",
    widgets: widgetCatalogItems([
      {
        id: "privacy-sensitive-reveal",
        type: "privacy.sensitiveFieldRevealAudit",
        title: "Sensitive field reveal audit",
        props: {
          items: [
            { label: "Compensation revealed", detail: "HRBP role", status: "info" },
          ],
        },
      },
      {
        id: "privacy-dsr",
        type: "privacy.dataSubjectRequestTracker",
        title: "Data subject request tracker",
        props: { steps: workflowSteps },
      },
      {
        id: "privacy-consent-expiration",
        type: "privacy.consentExpiration",
        title: "Consent expiration widget",
        props: {
          items: [
            {
              label: "Background consent",
              detail: "Expires in 14 days",
              status: "warning",
            },
          ],
        },
      },
      {
        id: "privacy-legal-hold",
        type: "privacy.retentionLegalHoldIndicator",
        title: "Retention / legal hold indicator",
        props: {
          items: [
            { label: "Legal hold active", status: "warning" },
            { label: "Purge blocked", status: "info" },
          ],
        },
      },
      {
        id: "privacy-access-review",
        type: "privacy.hrDataAccessReview",
        title: "HR data access review",
        props: {
          rows: [{ field: "Comp data", current: "12 viewers", proposed: "9 approved" }],
        },
      },
      {
        id: "privacy-impact",
        type: "privacy.privacyImpactAssessmentSummary",
        title: "Privacy impact assessment summary",
        props: {
          body: "Moderate privacy impact. Requires consent expiry and retention controls.",
          percent: 74,
        },
      },
    ]),
  }),
  catalogPage({
    id: "widgets-data-viz",
    title: "Data Visualization Widgets",
    description:
      "Chart widgets from standard line, bar, area, pie, scatter, and bubble charts through heatmaps, flow, distribution, pivot, and cross-tab summaries.",
    widgets: [
      widgetCatalogItem("viz-gauge", "viz.gauge", "Gauge", { value: 72, target: 85 }),
      widgetCatalogItem("viz-sparkline", "viz.sparkline", "Sparkline metric", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-line", "viz.lineChart", "Line chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-area", "viz.areaChart", "Area chart", {
        points: samplePoints,
      }),
      widgetCatalogItem(
        "viz-stacked-area",
        "viz.stackedAreaChart",
        "Stacked area chart",
        { points: samplePoints },
      ),
      widgetCatalogItem("viz-bar", "viz.barChart", "Bar chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-column", "viz.columnChart", "Column chart", {
        points: samplePoints,
      }),
      widgetCatalogItem(
        "viz-horizontal-bar",
        "viz.horizontalBarChart",
        "Horizontal bar chart",
        { points: samplePoints },
      ),
      widgetCatalogItem("viz-stacked-bar", "viz.stackedBarChart", "Stacked bar chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-grouped-bar", "viz.groupedBarChart", "Grouped bar chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-combo", "viz.comboChart", "Combo chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-pie", "viz.pieChart", "Pie chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-donut", "viz.donutChart", "Donut chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-polar-area", "viz.polarAreaChart", "Polar area chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-scatter", "viz.scatterPlot", "Scatter plot", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-bubble", "viz.bubbleChart", "Bubble chart", {
        points: samplePoints,
      }),
      widgetCatalogItem(
        "viz-packed-bubble",
        "viz.packedBubbleChart",
        "Packed bubble chart",
        { points: samplePoints },
      ),
      widgetCatalogItem("viz-bullet", "viz.bulletChart", "Bullet chart", {
        value: 72,
        target: 85,
      }),
      widgetCatalogItem("viz-box-plot", "viz.boxPlot", "Box and whisker plot", {
        points: samplePoints,
      }),
      widgetCatalogItem(
        "viz-candlestick",
        "viz.candlestickChart",
        "Candlestick chart",
        { points: samplePoints },
      ),
      widgetCatalogItem("viz-waterfall", "viz.waterfall", "Waterfall chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-pareto", "viz.paretoChart", "Pareto chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-funnel", "viz.funnel", "Funnel chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-heatmap", "viz.heatmap", "Heatmap", {
        points: samplePoints,
      }),
      widgetCatalogItem(
        "viz-calendar-heatmap",
        "viz.calendarHeatmap",
        "Calendar heatmap",
        { points: samplePoints },
      ),
      widgetCatalogItem("viz-radar", "viz.radar", "Radar/spider chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-sankey", "viz.sankey", "Sankey flow chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-treemap", "viz.treemap", "Treemap", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-sunburst", "viz.sunburstChart", "Sunburst chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-region-map", "viz.regionMap", "Map / region chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-timeline", "viz.timelineChart", "Timeline chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-gantt", "viz.ganttChart", "Gantt chart", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-cohort", "viz.cohortTable", "Cohort table", {
        rows: [{ field: "Q1", current: "82%", proposed: "88%" }],
      }),
      widgetCatalogItem("viz-histogram", "viz.histogram", "Distribution / histogram", {
        points: samplePoints,
      }),
      widgetCatalogItem("viz-pivot", "viz.pivotTable", "Pivot table", {
        rows: [{ field: "HRBP", current: 8, proposed: 11 }],
      }),
      widgetCatalogItem("viz-crosstab", "viz.crossTab", "Cross-tab summary", {
        rows: [{ field: "High risk", current: 3, proposed: 2 }],
      }),
    ],
  }),
  catalogPage({
    id: "widgets-time",
    title: "Clock And Timer Widgets",
    description:
      "Live clock, countdown, stopwatch, interval, SLA, schedule, and relative-time widgets for generated workflow surfaces.",
    widgets: [
      widgetCatalogItem("time-analog", "time.analogClock", "Analog clock", {
        label: "Local workflow time",
        offsetMinutes: 0,
      }),
      widgetCatalogItem("time-digital", "time.digitalClock", "Digital clock", {
        label: "Approver local time",
        offsetMinutes: 0,
      }),
      widgetCatalogItem("time-world", "time.worldClock", "World clock", {
        zones: timeZones,
      }),
      widgetCatalogItem(
        "time-zone-compare",
        "time.timezoneCompare",
        "Time-zone comparison",
        {
          zones: timeZones,
          body: "Shows working-hour overlap for distributed approvers.",
        },
      ),
      widgetCatalogItem("time-countdown", "time.countdownTimer", "Countdown timer", {
        label: "Payroll cutoff",
        durationSeconds: 900,
      }),
      widgetCatalogItem("time-countdown-ring", "time.countdownRing", "Countdown ring", {
        label: "Approval window",
        durationSeconds: 1800,
      }),
      widgetCatalogItem("time-stopwatch", "time.stopwatch", "Stopwatch", {
        label: "Review duration",
      }),
      widgetCatalogItem("time-lap", "time.lapTimer", "Lap timer", {
        label: "Step timing",
        items: [
          { label: "Intake", detail: "00:04:12" },
          { label: "Policy check", detail: "00:01:48" },
        ],
      }),
      widgetCatalogItem("time-duration", "time.durationTimer", "Duration timer", {
        label: "Elapsed in review",
        durationSeconds: 3600,
      }),
      widgetCatalogItem("time-sla", "time.slaTimer", "SLA timer", {
        label: "Compensation SLA",
        durationSeconds: 7200,
      }),
      widgetCatalogItem("time-pomodoro", "time.pomodoroTimer", "Pomodoro timer", {
        label: "Focus block",
        durationSeconds: 1500,
      }),
      widgetCatalogItem("time-interval", "time.intervalTimer", "Interval timer", {
        label: "Work / break interval",
        durationSeconds: 600,
        items: [
          { label: "Work", detail: "8 min" },
          { label: "Break", detail: "2 min" },
        ],
      }),
      widgetCatalogItem("time-cron", "time.cronSchedule", "Cron schedule", {
        items: [
          { label: "Payroll preview", detail: "0 7 * * MON-FRI" },
          { label: "Audit export", detail: "0 2 * * *" },
          { label: "SLA sweep", detail: "*/15 * * * *" },
        ],
      }),
      widgetCatalogItem(
        "time-business-hours",
        "time.businessHoursWindow",
        "Business-hours window",
        {
          label: "Finance review window",
          items: [
            { label: "Open", detail: "09:00" },
            { label: "Close", detail: "17:00" },
          ],
        },
      ),
      widgetCatalogItem("time-relative", "time.relativeTime", "Relative time / age", {
        items: [
          { label: "Submitted", detail: "14 minutes ago" },
          { label: "SLA breach", detail: "in 1 hour 42 minutes" },
          { label: "Last sync", detail: "37 seconds ago" },
        ],
      }),
    ],
  }),
  catalogPage({
    id: "widgets-documents-evidence",
    title: "Documents And Evidence Widgets",
    description:
      "Document, evidence, packet, redaction, OCR, and generated preview widgets.",
    widgets: [
      widgetCatalogItem(
        "doc-packet",
        "document.packetViewer",
        "Document packet viewer",
        {
          items: [
            { label: "Offer packet", status: "success" },
            { label: "Policy appendix", status: "info" },
          ],
        },
      ),
      widgetCatalogItem(
        "doc-diff",
        "document.comparisonDiff",
        "Document comparison/diff",
        { before: "Old pay band language", after: "Updated pay band language" },
      ),
      widgetCatalogItem(
        "doc-gallery",
        "document.attachmentGallery",
        "Attachment gallery",
        { items: [{ label: "manager-note.pdf" }, { label: "band-support.xlsx" }] },
      ),
      widgetCatalogItem(
        "doc-evidence-timeline",
        "document.evidenceTimeline",
        "Evidence timeline",
        { items: workflowSteps },
      ),
      widgetCatalogItem(
        "doc-signature-status",
        "document.signaturePacketStatus",
        "Signature packet status",
        { steps: workflowSteps },
      ),
      widgetCatalogItem(
        "doc-e-signature-canvas",
        "document.eSignatureCanvas",
        "E-signature canvas",
        {
          actor: "Jane Rivera",
          reason: "Compensation change attestation",
          value: { method: "drawn", attested: false, points: [] },
        },
      ),
      widgetCatalogItem(
        "doc-form-preview",
        "document.generatedFormPreview",
        "Form preview from generated schema",
        {
          rows: [
            { field: "Employee", current: "Jane Rivera", proposed: "Jane Rivera" },
          ],
        },
      ),
      widgetCatalogItem(
        "doc-pdf-preview",
        "document.generatedPdfPreview",
        "Generated PDF preview",
        { body: "Generated PDF packet preview with branded cover and signatures." },
      ),
      widgetCatalogItem(
        "doc-audit-export",
        "document.auditExportPreview",
        "Audit export preview",
        { code: "{ actor, transition, before, after, timestamp }" },
      ),
      widgetCatalogItem(
        "doc-redaction",
        "document.redactionPreview",
        "Redaction preview widget",
        { before: "SSN: 123-45-6789", after: "SSN: ***-**-6789" },
      ),
      widgetCatalogItem(
        "doc-ocr",
        "document.ocrExtractedFields",
        "OCR/extracted-fields review widget",
        { rows: [{ field: "Name", current: "Jane Rivera", proposed: "Jane Rivera" }] },
      ),
    ],
  }),
  catalogPage({
    id: "widgets-collaboration",
    title: "Collaboration Widgets",
    description:
      "Comments, notes, assignment, watchers, notifications, and meeting widgets for multi-party workflow review.",
    widgets: [
      widgetCatalogItem(
        "collab-comments",
        "collaboration.threadedComments",
        "Threaded comments",
        {
          messages: [
            { actor: "Riley HRBP", body: "Need comp signoff." },
            { actor: "Priya Finance", body: "Approved with cutoff note." },
          ],
        },
      ),
      widgetCatalogItem(
        "collab-feed",
        "collaboration.activityFeed",
        "Mentions/activity feed",
        { items: workflowSteps },
      ),
      widgetCatalogItem(
        "collab-notes",
        "collaboration.reviewerNotes",
        "Reviewer notes",
        { body: "Reviewer-private notes with workflow context." },
      ),
      widgetCatalogItem(
        "collab-rationale",
        "collaboration.decisionRationale",
        "Decision rationale panel",
        { body: "Decision rationale captured before transition." },
      ),
      widgetCatalogItem(
        "collab-assignment",
        "collaboration.assignment",
        "Assignment widget",
        {
          items: [
            { label: "Owner: Riley HRBP", status: "info" },
            { label: "Due: Today", status: "warning" },
          ],
        },
      ),
      widgetCatalogItem(
        "collab-watchers",
        "collaboration.watchers",
        "Watchers/subscribers widget",
        {
          items: [
            { label: "Compensation" },
            { label: "Payroll" },
            { label: "Finance" },
          ],
        },
      ),
      widgetCatalogItem(
        "collab-split",
        "collaboration.commentSplit",
        "Internal vs external comments split",
        { before: "Internal HRBP note", after: "Employee-safe message" },
      ),
      widgetCatalogItem(
        "collab-notification",
        "collaboration.notificationPreview",
        "Notification preview widget",
        { body: "Preview of email, Slack, or in-app notification." },
      ),
      widgetCatalogItem(
        "collab-meeting",
        "collaboration.meetingSchedule",
        "Meeting/schedule widget",
        {
          items: [{ label: "Comp review", detail: "May 16, 10:00 AM", status: "info" }],
        },
      ),
    ],
  }),
  catalogPage({
    id: "widgets-ai-governance",
    title: "AI And Governance Widgets",
    description:
      "AI review widgets for risk, confidence, missing data, citations, visibility, audit, overrides, fairness, and sensitive access.",
    widgets: [
      widgetCatalogItem("ai-risk", "ai.riskSummary", "AI risk summary", {
        body: "Medium risk due to payroll timing and access changes.",
        metrics: metricItems,
      }),
      widgetCatalogItem(
        "ai-confidence",
        "ai.confidenceExplanation",
        "AI confidence/explanation panel",
        {
          percent: 82,
          body: "High confidence based on complete comp and org context.",
        },
      ),
      widgetCatalogItem(
        "ai-next-action",
        "ai.suggestedNextAction",
        "AI suggested next action",
        { body: "Request payroll watcher before approval." },
      ),
      widgetCatalogItem(
        "ai-missing-data",
        "ai.missingDataDetector",
        "Missing-data detector",
        {
          items: [
            { label: "Payroll watcher", status: "warning" },
            { label: "Manager rationale", status: "success" },
          ],
        },
      ),
      widgetCatalogItem(
        "ai-policy-citation",
        "ai.policyCitationViewer",
        "Policy citation viewer",
        {
          items: [
            {
              label: "Comp Policy 4.2",
              detail: "Band exceptions require HRBP approval.",
            },
          ],
        },
      ),
      widgetCatalogItem(
        "ai-prompt-visibility",
        "ai.promptVisibility",
        "Prompt/input visibility widget",
        { code: "Visible fields: job, department, band, cutoff warning" },
      ),
      widgetCatalogItem(
        "ai-output-audit",
        "ai.modelOutputAudit",
        "Model output audit widget",
        {
          rows: [
            { field: "Model", current: "risk-review", proposed: "approved output" },
          ],
        },
      ),
      widgetCatalogItem(
        "ai-override",
        "ai.humanOverrideTracker",
        "Human override tracker",
        { items: [{ label: "Payroll warning overridden", status: "warning" }] },
      ),
      widgetCatalogItem(
        "ai-fairness",
        "ai.biasFairnessCheck",
        "Bias/fairness check widget",
        {
          metrics: [
            { label: "Variance", value: "2.1%" },
            { label: "Threshold", value: "5%" },
          ],
        },
      ),
      widgetCatalogItem(
        "ai-sensitive-log",
        "ai.sensitiveFieldAccessLog",
        "Sensitive-field access log",
        {
          items: [
            { label: "Compensation revealed", detail: "Riley HRBP", status: "info" },
          ],
        },
      ),
    ],
  }),
  catalogPage({
    id: "widgets-integration",
    title: "Integration Widgets",
    description:
      "Integration status, sync health, webhooks, payloads, mapping, repair, validation, and source-of-truth comparison.",
    widgets: [
      widgetCatalogItem(
        "int-system",
        "integration.externalSystemStatus",
        "External system status card",
        {
          items: [
            { label: "Workday", status: "success" },
            { label: "Payroll", status: "warning" },
          ],
        },
      ),
      widgetCatalogItem("int-sync", "integration.syncHealth", "Sync health widget", {
        percent: 91,
        metrics: [
          { label: "Lag", value: "4m" },
          { label: "Failures", value: 2 },
        ],
      }),
      widgetCatalogItem(
        "int-webhook",
        "integration.webhookDeliveryLog",
        "Webhook delivery log",
        {
          items: [
            { label: "employee.updated", status: "success" },
            { label: "payroll.write", status: "warning" },
          ],
        },
      ),
      widgetCatalogItem(
        "int-payload",
        "integration.apiPayloadPreview",
        "API payload preview",
        { code: "{ employeeId, effectiveDate, managerId, payRate }" },
      ),
      widgetCatalogItem(
        "int-mapping",
        "integration.fieldMappingPreview",
        "Field mapping preview",
        {
          rows: [
            {
              field: "managerId",
              current: "workflow.manager",
              proposed: "workday.manager_id",
            },
          ],
        },
      ),
      widgetCatalogItem(
        "int-repair",
        "integration.errorRepair",
        "Integration error repair widget",
        { items: [{ label: "Retry payroll write", status: "warning" }] },
      ),
      widgetCatalogItem(
        "int-validation",
        "integration.thirdPartyValidation",
        "Third-party validation result widget",
        { items: [{ label: "Market range check", status: "success" }] },
      ),
      widgetCatalogItem(
        "int-sot",
        "integration.sourceOfTruthComparison",
        "Source-of-truth comparison widget",
        { before: "Workflow: Jordan Lee", after: "Workday: Alex Manager" },
      ),
    ],
  }),
  catalogPage({
    id: "widgets-ui-blocks",
    title: "Broad UI Building Blocks",
    description: "Reusable layout and interaction blocks for generated workflow pages.",
    widgets: [
      widgetCatalogItem("ui-tabs", "ui.tabs", "Tabs", {
        items: [{ label: "Summary" }, { label: "Evidence" }, { label: "Audit" }],
      }),
      widgetCatalogItem("ui-accordion", "ui.accordion", "Accordion", {
        items: [{ label: "Policy details" }, { label: "Downstream impact" }],
      }),
      widgetCatalogItem("ui-stepper", "ui.stepper", "Stepper", {
        steps: workflowSteps,
      }),
      widgetCatalogItem("ui-wizard", "ui.wizardProgress", "Wizard progress", {
        steps: workflowSteps,
      }),
      widgetCatalogItem("ui-kanban", "ui.kanbanBoard", "Kanban board", {
        items: workflowSteps,
      }),
      widgetCatalogItem("ui-calendar", "ui.calendar", "Calendar", {
        items: [
          { label: "Cutoff", detail: "May 31" },
          { label: "Effective date", detail: "June 15" },
        ],
      }),
      widgetCatalogItem("ui-timeline", "ui.timelineVariant", "Timeline variants", {
        items: workflowSteps,
      }),
      widgetCatalogItem("ui-map-list", "ui.mapListSplit", "Map/list split", {
        items: [{ label: "Cambridge, MA" }, { label: "Somerville, MA" }],
      }),
      widgetCatalogItem("ui-command", "ui.commandPalette", "Command palette", {
        items: [
          { label: "Approve" },
          { label: "Request info" },
          { label: "Open audit" },
        ],
      }),
      widgetCatalogItem("ui-search", "ui.searchResults", "Search results widget", {
        items: [{ label: "Jane Rivera" }, { label: "Compensation policy" }],
      }),
      widgetCatalogItem(
        "ui-state",
        "ui.stateDisplay",
        "Empty/loading/error state widgets",
        {
          items: [
            { label: "Empty", status: "info" },
            { label: "Loading", status: "warning" },
            { label: "Error", status: "error" },
          ],
        },
      ),
      widgetCatalogItem("ui-toast", "ui.toastCenter", "Toast/notification center", {
        items: [
          { label: "Approval saved", status: "success" },
          { label: "Payroll warning", status: "warning" },
        ],
      }),
      widgetCatalogItem("ui-modal", "ui.modalDrawer", "Modal/drawer surface", {
        body: "Drawer-style detail panel with action footer.",
      }),
      widgetCatalogItem("ui-split", "ui.resizableSplitPane", "Resizable split pane", {
        before: "Form pane",
        after: "Preview pane",
      }),
    ],
  }),
];

export const initialPageDefinitions: readonly PageDefinition[] = [
  ...basePageDefinitions,
  ...typedControlPages,
  ...typedItemPages,
  ...expandedWidgetPages,
];

export const findInitialPageDefinition = (pageId: string): PageDefinition | undefined =>
  initialPageDefinitions.find((page) => page.id === pageId);

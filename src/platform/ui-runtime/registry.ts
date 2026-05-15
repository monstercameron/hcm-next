import type {
  SurfaceMode,
  WidgetDefinition,
  WidgetTrustTier,
} from "@hcm-next/ui-contracts";

const allSurfaces: readonly SurfaceMode[] = [
  "full_app",
  "customer_portal",
  "embedded_manager_widget",
  "embedded_approval_widget",
  "employee_self_service",
  "hrbp_workbench",
  "compensation_review",
  "payroll_review",
  "finance_review",
  "admin_preview",
  "audit_export",
  "mobile_compact",
  "email_summary",
];

const workflowSurfaces: readonly SurfaceMode[] = [
  "full_app",
  "customer_portal",
  "embedded_manager_widget",
  "embedded_approval_widget",
  "employee_self_service",
  "hrbp_workbench",
  "compensation_review",
  "payroll_review",
  "finance_review",
  "admin_preview",
  "mobile_compact",
];

const definition = (
  widgetType: string,
  displayName: string,
  tier: WidgetTrustTier,
  category: WidgetDefinition["category"],
  allowedSurfaces: readonly SurfaceMode[] = allSurfaces,
  requiredBindings: readonly string[] = [],
): WidgetDefinition => ({
  widgetType,
  displayName,
  tier,
  category,
  allowedSurfaces,
  defaultSize: "full",
  supportsResize: true,
  supportsDataBinding: true,
  supportsPersonalization: tier !== "governed_workflow",
  requiredBindings,
});

const domainDefinitions = (
  entries: readonly (readonly [
    widgetType: string,
    displayName: string,
    category: WidgetDefinition["category"],
    tier?: WidgetTrustTier,
  ])[],
): readonly WidgetDefinition[] =>
  entries.map(([widgetType, displayName, category, tier = "benign_content"]) =>
    definition(widgetType, displayName, tier, category),
  );

const hcmInsightWidgetDefinitions: readonly WidgetDefinition[] = [
  definition(
    "hcm.compensationBandVisualizer",
    "Compensation Band Visualizer",
    "benign_content",
    "hcm_context",
  ),
  definition("hcm.compaRatio", "Compa-Ratio", "benign_content", "data_display"),
  definition(
    "hcm.payEquityScatter",
    "Pay Equity Scatter",
    "benign_content",
    "data_display",
  ),
  definition(
    "hcm.payrollCutoffCalendar",
    "Payroll Cutoff Calendar",
    "benign_content",
    "hcm_context",
  ),
  definition(
    "hcm.retroPayPreview",
    "Retro Pay Preview",
    "benign_content",
    "hcm_context",
  ),
  definition(
    "hcm.benefitsEligibilityImpact",
    "Benefits Eligibility Impact",
    "benign_content",
    "hcm_context",
  ),
  definition(
    "hcm.jobProfileImpact",
    "Job Profile / Position Impact",
    "benign_content",
    "hcm_context",
  ),
  definition(
    "hcm.spanOfControl",
    "Span Of Control Chart",
    "benign_content",
    "hcm_context",
  ),
  definition(
    "hcm.reportingLineDiff",
    "Reporting-Line Diff",
    "benign_content",
    "change_review",
  ),
  definition(
    "hcm.accessRoleDelta",
    "Access / Role Delta",
    "benign_content",
    "hcm_context",
  ),
  definition(
    "hcm.licenseCertificationExpiry",
    "License Certification Expiry",
    "benign_content",
    "hcm_context",
  ),
  definition(
    "hcm.trainingCompletionMatrix",
    "Training Completion Matrix",
    "benign_content",
    "hcm_context",
  ),
  definition(
    "hcm.leaveBalanceImpact",
    "Leave Balance Impact",
    "benign_content",
    "hcm_context",
  ),
  definition(
    "hcm.workAuthorizationStatus",
    "I-9 / Work Authorization Status",
    "benign_content",
    "hcm_context",
  ),
  definition(
    "hcm.unionContractImpact",
    "Union / Contract Rule Impact",
    "benign_content",
    "hcm_context",
  ),
  definition(
    "hcm.locationCompliance",
    "Location / Legal Entity Compliance",
    "benign_content",
    "hcm_context",
  ),
];

const workflowOpsWidgetDefinitions: readonly WidgetDefinition[] = [
  definition(
    "workflow.slaCountdown",
    "SLA Countdown",
    "governed_workflow",
    "transaction",
  ),
  definition(
    "workflow.escalationPath",
    "Escalation Path",
    "governed_workflow",
    "approval",
  ),
  definition(
    "workflow.approvalSwimlane",
    "Approval Chain Swimlane",
    "governed_workflow",
    "approval",
  ),
  definition(
    "workflow.parallelApprovalBoard",
    "Parallel Approval Status Board",
    "governed_workflow",
    "approval",
  ),
  definition(
    "workflow.requiredEvidenceChecklist",
    "Required Evidence Checklist",
    "governed_workflow",
    "transaction",
  ),
  definition(
    "workflow.blockingIssuesPanel",
    "Blocking Issues Panel",
    "governed_workflow",
    "transaction",
  ),
  definition(
    "workflow.dependencyGraph",
    "Dependency Graph",
    "governed_workflow",
    "transaction",
  ),
  definition(
    "workflow.repairQueue",
    "Retry / Repair Queue",
    "governed_workflow",
    "transaction",
  ),
  definition(
    "workflow.stateMachineViewer",
    "State Machine Viewer",
    "governed_workflow",
    "transaction",
  ),
  definition(
    "workflow.transitionHistory",
    "Transition History",
    "governed_workflow",
    "audit",
  ),
  definition(
    "workflow.changedSinceLastReview",
    "Changed Since Last Review",
    "governed_workflow",
    "change_review",
  ),
  definition(
    "workflow.taskHandoff",
    "Task Handoff",
    "governed_workflow",
    "transaction",
  ),
  definition(
    "workflow.exceptionQueue",
    "Exception Queue",
    "governed_workflow",
    "transaction",
  ),
  definition(
    "workflow.policyFindings",
    "Policy Findings Panel",
    "governed_workflow",
    "approval",
  ),
];

const dataVizWidgetDefinitions: readonly WidgetDefinition[] = [
  definition("viz.gauge", "Gauge", "benign_content", "data_display"),
  definition("viz.sparkline", "Sparkline Metric", "benign_content", "data_display"),
  definition("viz.lineChart", "Line Chart", "benign_content", "data_display"),
  definition("viz.areaChart", "Area Chart", "benign_content", "data_display"),
  definition(
    "viz.stackedAreaChart",
    "Stacked Area Chart",
    "benign_content",
    "data_display",
  ),
  definition("viz.barChart", "Bar Chart", "benign_content", "data_display"),
  definition("viz.columnChart", "Column Chart", "benign_content", "data_display"),
  definition(
    "viz.horizontalBarChart",
    "Horizontal Bar Chart",
    "benign_content",
    "data_display",
  ),
  definition(
    "viz.stackedBarChart",
    "Stacked Bar Chart",
    "benign_content",
    "data_display",
  ),
  definition(
    "viz.groupedBarChart",
    "Grouped Bar Chart",
    "benign_content",
    "data_display",
  ),
  definition("viz.comboChart", "Combo Chart", "benign_content", "data_display"),
  definition("viz.pieChart", "Pie Chart", "benign_content", "data_display"),
  definition("viz.donutChart", "Donut Chart", "benign_content", "data_display"),
  definition(
    "viz.polarAreaChart",
    "Polar Area Chart",
    "benign_content",
    "data_display",
  ),
  definition("viz.scatterPlot", "Scatter Plot", "benign_content", "data_display"),
  definition("viz.bubbleChart", "Bubble Chart", "benign_content", "data_display"),
  definition(
    "viz.packedBubbleChart",
    "Packed Bubble Chart",
    "benign_content",
    "data_display",
  ),
  definition("viz.bulletChart", "Bullet Chart", "benign_content", "data_display"),
  definition("viz.boxPlot", "Box And Whisker Plot", "benign_content", "data_display"),
  definition(
    "viz.candlestickChart",
    "Candlestick Chart",
    "benign_content",
    "data_display",
  ),
  definition("viz.waterfall", "Waterfall Chart", "benign_content", "data_display"),
  definition("viz.paretoChart", "Pareto Chart", "benign_content", "data_display"),
  definition("viz.funnel", "Funnel Chart", "benign_content", "data_display"),
  definition("viz.heatmap", "Heatmap", "benign_content", "data_display"),
  definition(
    "viz.calendarHeatmap",
    "Calendar Heatmap",
    "benign_content",
    "data_display",
  ),
  definition("viz.radar", "Radar Chart", "benign_content", "data_display"),
  definition("viz.sankey", "Sankey Flow Chart", "benign_content", "data_display"),
  definition("viz.treemap", "Treemap", "benign_content", "data_display"),
  definition("viz.sunburstChart", "Sunburst Chart", "benign_content", "data_display"),
  definition("viz.regionMap", "Map / Region Chart", "benign_content", "data_display"),
  definition("viz.timelineChart", "Timeline Chart", "benign_content", "data_display"),
  definition("viz.ganttChart", "Gantt Chart", "benign_content", "data_display"),
  definition("viz.cohortTable", "Cohort Table", "benign_content", "data_display"),
  definition(
    "viz.histogram",
    "Distribution / Histogram",
    "benign_content",
    "data_display",
  ),
  definition("viz.pivotTable", "Pivot Table", "benign_content", "data_display"),
  definition("viz.crossTab", "Cross-Tab Summary", "benign_content", "data_display"),
];

const timeWidgetDefinitions: readonly WidgetDefinition[] = [
  definition("time.analogClock", "Analog Clock", "benign_content", "data_display"),
  definition("time.digitalClock", "Digital Clock", "benign_content", "data_display"),
  definition("time.worldClock", "World Clock", "benign_content", "data_display"),
  definition(
    "time.timezoneCompare",
    "Time Zone Comparison",
    "benign_content",
    "data_display",
  ),
  definition("time.countdownTimer", "Countdown Timer", "benign_content", "transaction"),
  definition("time.countdownRing", "Countdown Ring", "benign_content", "transaction"),
  definition("time.stopwatch", "Stopwatch", "benign_content", "transaction"),
  definition("time.lapTimer", "Lap Timer", "benign_content", "transaction"),
  definition("time.durationTimer", "Duration Timer", "benign_content", "transaction"),
  definition("time.slaTimer", "SLA Timer", "governed_workflow", "transaction"),
  definition("time.pomodoroTimer", "Pomodoro Timer", "benign_content", "transaction"),
  definition("time.intervalTimer", "Interval Timer", "benign_content", "transaction"),
  definition("time.cronSchedule", "Cron Schedule", "benign_content", "data_display"),
  definition(
    "time.businessHoursWindow",
    "Business Hours Window",
    "benign_content",
    "data_display",
  ),
  definition(
    "time.relativeTime",
    "Relative Time / Age",
    "benign_content",
    "data_display",
  ),
];

const documentWidgetDefinitions: readonly WidgetDefinition[] = [
  definition(
    "document.packetViewer",
    "Document Packet Viewer",
    "benign_content",
    "media",
  ),
  definition(
    "document.comparisonDiff",
    "Document Comparison / Diff",
    "benign_content",
    "change_review",
  ),
  definition(
    "document.attachmentGallery",
    "Attachment Gallery",
    "benign_content",
    "media",
  ),
  definition(
    "document.evidenceTimeline",
    "Evidence Timeline",
    "benign_content",
    "audit",
  ),
  definition(
    "document.signaturePacketStatus",
    "Signature Packet Status",
    "benign_content",
    "transaction",
  ),
  definition(
    "document.eSignatureCanvas",
    "E-Signature Canvas",
    "benign_content",
    "transaction",
  ),
  definition(
    "document.generatedFormPreview",
    "Generated Form Preview",
    "benign_content",
    "form",
  ),
  definition(
    "document.generatedPdfPreview",
    "Generated PDF Preview",
    "benign_content",
    "media",
  ),
  definition(
    "document.auditExportPreview",
    "Audit Export Preview",
    "benign_content",
    "audit",
  ),
  definition(
    "document.redactionPreview",
    "Redaction Preview",
    "benign_content",
    "media",
  ),
  definition(
    "document.ocrExtractedFields",
    "OCR Extracted Fields Review",
    "benign_content",
    "data_display",
  ),
];

const collaborationWidgetDefinitions: readonly WidgetDefinition[] = [
  definition(
    "collaboration.threadedComments",
    "Threaded Comments",
    "benign_content",
    "content",
  ),
  definition(
    "collaboration.activityFeed",
    "Mentions / Activity Feed",
    "benign_content",
    "audit",
  ),
  definition(
    "collaboration.reviewerNotes",
    "Reviewer Notes",
    "benign_content",
    "content",
  ),
  definition(
    "collaboration.decisionRationale",
    "Decision Rationale Panel",
    "governed_workflow",
    "approval",
  ),
  definition(
    "collaboration.assignment",
    "Assignment Widget",
    "governed_workflow",
    "transaction",
  ),
  definition(
    "collaboration.watchers",
    "Watchers / Subscribers",
    "benign_content",
    "content",
  ),
  definition(
    "collaboration.commentSplit",
    "Internal / External Comments Split",
    "benign_content",
    "content",
  ),
  definition(
    "collaboration.notificationPreview",
    "Notification Preview",
    "benign_content",
    "content",
  ),
  definition(
    "collaboration.meetingSchedule",
    "Meeting / Schedule Widget",
    "benign_content",
    "transaction",
  ),
];

const aiGovernanceWidgetDefinitions: readonly WidgetDefinition[] = [
  definition("ai.riskSummary", "AI Risk Summary", "governed_workflow", "ai_review"),
  definition(
    "ai.confidenceExplanation",
    "AI Confidence / Explanation",
    "governed_workflow",
    "ai_review",
  ),
  definition(
    "ai.suggestedNextAction",
    "AI Suggested Next Action",
    "governed_workflow",
    "ai_review",
  ),
  definition(
    "ai.missingDataDetector",
    "Missing Data Detector",
    "governed_workflow",
    "ai_review",
  ),
  definition(
    "ai.policyCitationViewer",
    "Policy Citation Viewer",
    "governed_workflow",
    "ai_review",
  ),
  definition(
    "ai.promptVisibility",
    "Prompt / Input Visibility",
    "governed_workflow",
    "ai_review",
  ),
  definition(
    "ai.modelOutputAudit",
    "Model Output Audit",
    "governed_workflow",
    "ai_review",
  ),
  definition(
    "ai.humanOverrideTracker",
    "Human Override Tracker",
    "governed_workflow",
    "ai_review",
  ),
  definition(
    "ai.biasFairnessCheck",
    "Bias / Fairness Check",
    "governed_workflow",
    "ai_review",
  ),
  definition(
    "ai.sensitiveFieldAccessLog",
    "Sensitive Field Access Log",
    "governed_workflow",
    "audit",
  ),
];

const integrationWidgetDefinitions: readonly WidgetDefinition[] = [
  definition(
    "integration.externalSystemStatus",
    "External System Status",
    "benign_content",
    "external_embed",
  ),
  definition(
    "integration.syncHealth",
    "Sync Health",
    "benign_content",
    "external_embed",
  ),
  definition(
    "integration.webhookDeliveryLog",
    "Webhook Delivery Log",
    "benign_content",
    "external_embed",
  ),
  definition(
    "integration.apiPayloadPreview",
    "API Payload Preview",
    "benign_content",
    "external_embed",
  ),
  definition(
    "integration.fieldMappingPreview",
    "Field Mapping Preview",
    "benign_content",
    "external_embed",
  ),
  definition(
    "integration.errorRepair",
    "Integration Error Repair",
    "governed_workflow",
    "external_embed",
  ),
  definition(
    "integration.thirdPartyValidation",
    "Third-Party Validation Result",
    "benign_content",
    "external_embed",
  ),
  definition(
    "integration.sourceOfTruthComparison",
    "Source-Of-Truth Comparison",
    "benign_content",
    "external_embed",
  ),
];

const recruitingWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["recruiting.candidatePipelineBoard", "Candidate Pipeline Board", "data_display"],
  ["recruiting.requisitionHealthCard", "Requisition Health Card", "hcm_context"],
  ["recruiting.interviewSchedulePanel", "Interview Schedule Panel", "transaction"],
  [
    "recruiting.interviewScorecardSummary",
    "Interview Scorecard Summary",
    "data_display",
  ],
  ["recruiting.offerPackageComparison", "Offer Package Comparison", "change_review"],
  [
    "recruiting.backgroundCheckStatus",
    "Background Check Status",
    "transaction",
    "governed_workflow",
  ],
  [
    "recruiting.candidateCommunicationsTimeline",
    "Candidate Communications Timeline",
    "audit",
  ],
]);

const onboardingWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["onboarding.newHireJourneyTracker", "New-Hire Journey Tracker", "transaction"],
  ["onboarding.taskBundle", "Onboarding Task Bundle", "transaction"],
  [
    "onboarding.provisioningChecklist",
    "Equipment / Access Provisioning Checklist",
    "transaction",
  ],
  ["onboarding.firstWeekSchedule", "First-Week Schedule", "data_display"],
  ["onboarding.requiredDocuments", "Required Document Completion", "transaction"],
  ["onboarding.buddyMentorAssignment", "Buddy / Mentor Assignment", "hcm_context"],
]);

const offboardingWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["offboarding.terminationChecklist", "Termination Checklist", "transaction"],
  ["offboarding.exitInterviewSummary", "Exit Interview Summary", "hcm_context"],
  ["offboarding.knowledgeTransferTracker", "Knowledge Transfer Tracker", "transaction"],
  ["offboarding.assetReturn", "Asset Return Widget", "transaction"],
  [
    "offboarding.accessRevocationStatus",
    "Access Revocation Status",
    "transaction",
    "governed_workflow",
  ],
  [
    "offboarding.finalPaySeveranceChecklist",
    "Final Pay / Severance Checklist",
    "transaction",
  ],
]);

const performanceWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["performance.goalsOkrTracker", "Goals / OKR Tracker", "hcm_context"],
  ["performance.reviewCycleStatus", "Review Cycle Status", "transaction"],
  ["performance.ratingDistribution", "Rating Distribution", "data_display"],
  ["performance.calibrationMatrix", "Calibration Matrix", "data_display"],
  ["performance.feedbackTimeline", "Feedback Timeline", "audit"],
  [
    "performance.improvementPlanTracker",
    "Performance Improvement Plan Tracker",
    "transaction",
    "governed_workflow",
  ],
  ["performance.promotionReadinessCard", "Promotion Readiness Card", "hcm_context"],
]);

const talentWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["talent.nineBoxGrid", "9-Box Talent Grid", "data_display"],
  ["talent.successionBench", "Succession Bench Widget", "hcm_context"],
  ["talent.skillsInventory", "Skills Inventory", "hcm_context"],
  ["talent.competencyHeatmap", "Competency Heatmap", "data_display"],
  ["talent.internalMobilityMatch", "Internal Mobility Match Card", "hcm_context"],
  ["talent.talentPoolRoster", "Talent Pool Roster", "data_display"],
  [
    "talent.flightRetentionRisk",
    "Flight-Risk / Retention Risk Widget",
    "ai_review",
    "governed_workflow",
  ],
]);

const workforceWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["workforce.headcountPlanActual", "Headcount Plan Versus Actual", "data_display"],
  ["workforce.positionControl", "Position Control Widget", "hcm_context"],
  ["workforce.vacancyAging", "Vacancy Aging", "data_display"],
  ["workforce.scenarioPlanner", "Workforce Scenario Planner", "transaction"],
  ["workforce.budgetedFilledRoles", "Budgeted Versus Filled Roles", "data_display"],
  ["workforce.attritionForecast", "Attrition Forecast", "ai_review"],
  ["workforce.spanLayerAnalysis", "Span / Layer Analysis", "hcm_context"],
]);

const schedulingWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["scheduling.shiftScheduleGrid", "Shift Schedule Grid", "data_display"],
  ["scheduling.timecardExceptionPanel", "Timecard Exception Panel", "transaction"],
  ["scheduling.overtimeRisk", "Overtime Risk Widget", "ai_review"],
  ["scheduling.missedPunchQueue", "Missed Punch Queue", "transaction"],
  ["scheduling.attendancePatternDetector", "Attendance Pattern Detector", "ai_review"],
  ["scheduling.coverageGapHeatmap", "Coverage Gap Heatmap", "data_display"],
  ["scheduling.leaveCalendarOverlay", "Leave Calendar Overlay", "data_display"],
]);

const leaveWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["leave.requestStatus", "Leave Request Status", "transaction"],
  ["leave.fmlaLoaCaseTracker", "FMLA / LOA Case Tracker", "transaction"],
  ["leave.intermittentUsage", "Intermittent Leave Usage", "data_display"],
  ["leave.returnToWorkChecklist", "Return-To-Work Checklist", "transaction"],
  ["leave.absenceTrend", "Absence Trend Widget", "data_display"],
  ["leave.accrualForecast", "Accrual Forecast", "data_display"],
  ["leave.conflictDetector", "Leave Conflict Detector", "ai_review"],
]);

const benefitsWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["benefits.enrollmentProgress", "Benefits Enrollment Progress", "transaction"],
  ["benefits.lifeEventImpact", "Life-Event Impact Widget", "change_review"],
  [
    "benefits.dependentVerificationStatus",
    "Dependent Verification Status",
    "transaction",
  ],
  [
    "benefits.openEnrollmentDecisionSupport",
    "Open Enrollment Decision Support",
    "hcm_context",
  ],
  ["benefits.costComparison", "Benefits Cost Comparison", "data_display"],
  ["benefits.cobraEligibilityTracker", "COBRA Eligibility Tracker", "hcm_context"],
]);

const payrollWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["payroll.grossToNetPreview", "Gross-To-Net Preview", "data_display"],
  ["payroll.payStatementPreview", "Pay Statement Preview", "media"],
  [
    "payroll.exceptionQueue",
    "Payroll Exception Queue",
    "transaction",
    "governed_workflow",
  ],
  ["payroll.taxJurisdictionImpact", "Tax Jurisdiction Impact", "change_review"],
  [
    "payroll.garnishmentDeductionSummary",
    "Garnishment / Deduction Summary",
    "hcm_context",
  ],
  ["payroll.retroPayDetailBreakdown", "Retro Pay Detail Breakdown", "data_display"],
  ["payroll.reconciliation", "Payroll Reconciliation Widget", "transaction"],
]);

const employeeRelationsWidgetDefinitions: readonly WidgetDefinition[] =
  domainDefinitions([
    [
      "employeeRelations.caseStatus",
      "HR Case Status",
      "transaction",
      "governed_workflow",
    ],
    [
      "employeeRelations.investigationTimeline",
      "Investigation Timeline",
      "audit",
      "governed_workflow",
    ],
    ["employeeRelations.grievanceTracker", "Grievance Tracker", "transaction"],
    [
      "employeeRelations.accommodationRequestTracker",
      "Accommodation Request Tracker",
      "transaction",
      "governed_workflow",
    ],
    [
      "employeeRelations.disciplinaryActionHistory",
      "Disciplinary Action History",
      "audit",
      "governed_workflow",
    ],
    [
      "employeeRelations.policyViolationSummary",
      "Policy Violation Summary",
      "hcm_context",
    ],
    [
      "employeeRelations.confidentialNotesPanel",
      "Confidential Notes Panel",
      "content",
      "governed_workflow",
    ],
  ]);

const complianceWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  [
    "compliance.policyAttestationTracker",
    "Mandatory Policy Attestation Tracker",
    "transaction",
  ],
  ["compliance.auditReadinessScore", "Audit Readiness Score", "audit"],
  ["compliance.regulatoryDeadlineTracker", "Regulatory Deadline Tracker", "audit"],
  [
    "compliance.protectedClassImpactSummary",
    "Protected Class Impact Summary",
    "ai_review",
    "governed_workflow",
  ],
  ["compliance.recordRetentionStatus", "Record Retention Status", "audit"],
  ["compliance.consentDisclosureStatus", "Consent / Disclosure Status", "transaction"],
]);

const experienceWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["experience.pulseSurveyResults", "Pulse Survey Results", "data_display"],
  ["experience.engagementSentimentTrend", "Engagement Sentiment Trend", "data_display"],
  ["experience.enps", "eNPS Widget", "data_display"],
  ["experience.recognitionWall", "Recognition Wall", "content"],
  [
    "experience.internalCommsAnnouncement",
    "Internal Comms Announcement Widget",
    "content",
  ],
  [
    "experience.burnoutWorkloadSignal",
    "Burnout / Workload Signal",
    "ai_review",
    "governed_workflow",
  ],
]);

const coreHrisWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["coreHris.employee360Profile", "Employee 360 Profile", "hcm_context"],
  [
    "coreHris.workerDataCompletenessScore",
    "Worker Data Completeness Score",
    "data_display",
  ],
  ["coreHris.employmentHistoryTimeline", "Employment History Timeline", "audit"],
  [
    "coreHris.personalInfoChangeSummary",
    "Personal Info Change Summary",
    "change_review",
  ],
  ["coreHris.emergencyContactCard", "Emergency Contact Card", "hcm_context"],
  ["coreHris.directDepositStatus", "Direct Deposit Status", "transaction"],
  ["coreHris.jobPositionHistory", "Job / Position History", "audit"],
  ["coreHris.managerTeamRoster", "Manager Team Roster", "hcm_context"],
  [
    "coreHris.orgMembershipCostCenterHistory",
    "Org Membership / Cost Center History",
    "audit",
  ],
]);

const compensationAdvancedWidgetDefinitions: readonly WidgetDefinition[] =
  domainDefinitions([
    ["compAdvanced.totalRewardsStatement", "Total Rewards Statement", "hcm_context"],
    [
      "compAdvanced.meritCycleBudgetTracker",
      "Merit Cycle Budget Tracker",
      "data_display",
    ],
    [
      "compAdvanced.bonusIncentiveCalculator",
      "Bonus / Incentive Payout Calculator",
      "transaction",
    ],
    ["compAdvanced.equityGrantVesting", "Equity Grant / Vesting Widget", "hcm_context"],
    ["compAdvanced.salaryHistoryTimeline", "Salary History Timeline", "audit"],
    ["compAdvanced.rangePenetration", "Range Penetration Widget", "data_display"],
    [
      "compAdvanced.shiftDifferentialPremiumPay",
      "Shift Differential / Premium Pay Widget",
      "hcm_context",
    ],
    ["compAdvanced.compBudgetBurndown", "Comp Budget Burn-Down", "data_display"],
    [
      "compAdvanced.marketAdjustmentRecommendation",
      "Market Adjustment Recommendation",
      "ai_review",
      "governed_workflow",
    ],
  ]);

const learningWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["learning.learningPlanTracker", "Learning Plan Tracker", "transaction"],
  ["learning.lmsCourseCatalog", "LMS Course Catalog Widget", "content"],
  ["learning.skillGapAnalysis", "Skill Gap Analysis", "data_display"],
  [
    "learning.certificationComplianceDashboard",
    "Certification Compliance Dashboard",
    "hcm_context",
  ],
  [
    "learning.continuingEducationCredits",
    "CEU / Continuing Education Credits",
    "data_display",
  ],
  ["learning.trainingWaitlist", "Training Waitlist", "transaction"],
  ["learning.managerCoachingPlan", "Manager Coaching Plan", "hcm_context"],
  ["learning.careerPathVisualizer", "Career Path Visualizer", "hcm_context"],
]);

const deiWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["dei.representationDashboard", "Representation Dashboard", "data_display"],
  ["dei.diversityHiringFunnel", "Diversity Hiring Funnel", "data_display"],
  [
    "dei.adverseImpactAnalysis",
    "Adverse Impact Analysis",
    "ai_review",
    "governed_workflow",
  ],
  ["dei.payEquityCohortExplorer", "Pay Equity Cohort Explorer", "data_display"],
  ["dei.promotionEquityTracker", "Promotion Equity Tracker", "data_display"],
  ["dei.inclusionSurveyBreakdown", "Inclusion Survey Breakdown", "data_display"],
  ["dei.accommodationTrendDashboard", "Accommodation Trend Dashboard", "data_display"],
]);

const laborWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["labor.seniorityRoster", "Seniority Roster", "hcm_context"],
  [
    "labor.collectiveBargainingAgreementRuleSummary",
    "Collective Bargaining Agreement Rule Summary",
    "hcm_context",
  ],
  ["labor.shiftBidBoard", "Shift Bid Board", "transaction"],
  ["labor.bumpingRecallRights", "Bumping / Recall Rights Widget", "hcm_context"],
  ["labor.unionDuesStatus", "Union Dues Status", "transaction"],
  [
    "labor.grievanceStageBoard",
    "Grievance Stage Board",
    "transaction",
    "governed_workflow",
  ],
  ["labor.contractEligibilityChecker", "Contract Eligibility Checker", "hcm_context"],
]);

const healthSafetyWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  [
    "healthSafety.incidentReportTracker",
    "Incident Report Tracker",
    "transaction",
    "governed_workflow",
  ],
  ["healthSafety.oshaLogSummary", "OSHA Log Summary", "audit"],
  ["healthSafety.workersCompCaseTracker", "Workers' Comp Case Tracker", "transaction"],
  ["healthSafety.returnToDutyStatus", "Return-To-Duty Status", "transaction"],
  [
    "healthSafety.safetyTrainingCompliance",
    "Safety Training Compliance",
    "hcm_context",
  ],
  [
    "healthSafety.exposureVaccinationStatus",
    "Exposure / Vaccination Status",
    "hcm_context",
  ],
  [
    "healthSafety.workplaceInjuryTrend",
    "Workplace Injury Trend Widget",
    "data_display",
  ],
]);

const serviceDeliveryWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions(
  [
    [
      "serviceDelivery.knowledgeArticleViewer",
      "HR Knowledge Article Viewer",
      "content",
    ],
    ["serviceDelivery.caseSlaDashboard", "Case SLA Dashboard", "data_display"],
    ["serviceDelivery.caseCategorization", "Case Categorization Widget", "ai_review"],
    [
      "serviceDelivery.chatbotTranscriptPanel",
      "HR Chatbot Transcript Panel",
      "content",
    ],
    [
      "serviceDelivery.escalationReasonSummary",
      "Escalation Reason Summary",
      "hcm_context",
    ],
    [
      "serviceDelivery.serviceCenterVolumeForecast",
      "Service Center Volume Forecast",
      "data_display",
    ],
    ["serviceDelivery.caseCsatSurvey", "CSAT After-Case Survey Widget", "data_display"],
  ],
);

const employeeFinanceWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions(
  [
    [
      "employeeFinance.expenseApprovalSummary",
      "Expense Approval Summary",
      "transaction",
    ],
    [
      "employeeFinance.relocationPackageTracker",
      "Relocation Package Tracker",
      "transaction",
    ],
    [
      "employeeFinance.tuitionReimbursementTracker",
      "Tuition Reimbursement Tracker",
      "transaction",
    ],
    ["employeeFinance.referralBonusTracker", "Referral Bonus Tracker", "transaction"],
    ["employeeFinance.commuterBenefitUsage", "Commuter Benefit Usage", "data_display"],
    [
      "employeeFinance.payrollAdvanceRequestStatus",
      "Payroll Advance Request Status",
      "transaction",
    ],
  ],
);

const globalMobilityWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  ["globalMobility.visaSponsorshipTracker", "Visa Sponsorship Tracker", "transaction"],
  [
    "globalMobility.immigrationMilestoneTimeline",
    "Immigration Milestone Timeline",
    "audit",
  ],
  [
    "globalMobility.globalAssignmentPackage",
    "Global Assignment Package",
    "hcm_context",
  ],
  [
    "globalMobility.expatTaxEqualizationSummary",
    "Expat Tax Equalization Summary",
    "hcm_context",
  ],
  [
    "globalMobility.countryComplianceChecklist",
    "Country-Specific Compliance Checklist",
    "transaction",
  ],
  ["globalMobility.relocationMoveTracker", "Relocation Move Tracker", "transaction"],
]);

const privacyWidgetDefinitions: readonly WidgetDefinition[] = domainDefinitions([
  [
    "privacy.sensitiveFieldRevealAudit",
    "Sensitive Field Reveal Audit",
    "audit",
    "governed_workflow",
  ],
  [
    "privacy.dataSubjectRequestTracker",
    "Data Subject Request Tracker",
    "transaction",
    "governed_workflow",
  ],
  ["privacy.consentExpiration", "Consent Expiration Widget", "transaction"],
  ["privacy.retentionLegalHoldIndicator", "Retention / Legal Hold Indicator", "audit"],
  ["privacy.hrDataAccessReview", "HR Data Access Review", "audit", "governed_workflow"],
  [
    "privacy.privacyImpactAssessmentSummary",
    "Privacy Impact Assessment Summary",
    "audit",
  ],
]);

const uiBlockWidgetDefinitions: readonly WidgetDefinition[] = [
  definition("ui.tabs", "Tabs", "benign_content", "layout"),
  definition("ui.accordion", "Accordion", "benign_content", "layout"),
  definition("ui.stepper", "Stepper", "benign_content", "layout"),
  definition("ui.wizardProgress", "Wizard Progress", "benign_content", "layout"),
  definition("ui.kanbanBoard", "Kanban Board", "benign_content", "layout"),
  definition("ui.calendar", "Calendar", "benign_content", "layout"),
  definition("ui.timelineVariant", "Timeline Variants", "benign_content", "layout"),
  definition("ui.mapListSplit", "Map / List Split", "benign_content", "layout"),
  definition("ui.commandPalette", "Command Palette", "benign_content", "layout"),
  definition("ui.searchResults", "Search Results", "benign_content", "layout"),
  definition(
    "ui.stateDisplay",
    "Empty / Loading / Error States",
    "benign_content",
    "layout",
  ),
  definition(
    "ui.toastCenter",
    "Toast / Notification Center",
    "benign_content",
    "layout",
  ),
  definition("ui.modalDrawer", "Modal / Drawer Surface", "benign_content", "layout"),
  definition(
    "ui.resizableSplitPane",
    "Resizable Split Pane",
    "benign_content",
    "layout",
  ),
];

export const createDefaultWidgetRegistry = (): readonly WidgetDefinition[] => [
  definition("layout.section", "Section", "benign_content", "layout"),
  definition("layout.stack", "Stack", "benign_content", "layout"),
  definition("layout.grid", "Grid", "benign_content", "layout"),
  definition("queue.requestList", "Request Queue", "governed_workflow", "data_display"),
  definition(
    "employee.summary",
    "Employee Summary",
    "governed_workflow",
    "hcm_context",
    workflowSurfaces,
    ["employee"],
  ),
  definition(
    "form.dynamicFieldGroup",
    "Dynamic Field Group",
    "governed_workflow",
    "form",
  ),
  definition(
    "change.diff",
    "Current Versus Proposed Diff",
    "governed_workflow",
    "change_review",
  ),
  definition(
    "approval.decisionPanel",
    "Approval Decision Panel",
    "governed_workflow",
    "approval",
  ),
  definition(
    "simulation.resultPanel",
    "Simulation Result Panel",
    "governed_workflow",
    "transaction",
  ),
  definition("audit.timeline", "Audit Timeline", "governed_workflow", "audit"),
  definition("ai.changeBrief", "AI Change Brief", "governed_workflow", "ai_review"),
  definition("content.text", "Text Block", "benign_content", "content"),
  definition("content.markdown", "Markdown Viewer", "benign_content", "content"),
  definition("content.html", "Sanitized HTML Viewer", "benign_content", "content"),
  definition("content.callout", "Callout", "benign_content", "content"),
  definition("content.linkList", "Link List", "benign_content", "content"),
  definition("content.metricTile", "Metric Tile", "benign_content", "data_display"),
  definition(
    "content.labelValueList",
    "Label/Value List",
    "benign_content",
    "data_display",
  ),
  definition("content.faq", "FAQ", "benign_content", "content"),
  definition("data.progress", "Progress", "benign_content", "data_display"),
  definition("data.metricGraph", "Metric Graph", "benign_content", "data_display"),
  definition("data.graphChart", "Graph Chart", "benign_content", "data_display"),
  definition("data.nodeGraph", "Node Graph", "benign_content", "data_display"),
  definition("data.orgChart", "Org Chart", "benign_content", "hcm_context"),
  definition("data.table", "Table", "benign_content", "data_display"),
  definition(
    "data.filterableTable",
    "Filterable Table",
    "benign_content",
    "data_display",
  ),
  definition("data.matrix", "Matrix", "benign_content", "data_display"),
  definition("data.clusterBoard", "Cluster Board", "benign_content", "data_display"),
  definition("media.image", "Image", "benign_content", "media"),
  definition("media.audio", "Audio Player", "benign_content", "media"),
  definition("media.video", "Video Player", "benign_content", "media"),
  definition("media.pdf", "PDF Preview", "benign_content", "media"),
  ...hcmInsightWidgetDefinitions,
  ...workflowOpsWidgetDefinitions,
  ...dataVizWidgetDefinitions,
  ...timeWidgetDefinitions,
  ...documentWidgetDefinitions,
  ...collaborationWidgetDefinitions,
  ...aiGovernanceWidgetDefinitions,
  ...integrationWidgetDefinitions,
  ...recruitingWidgetDefinitions,
  ...onboardingWidgetDefinitions,
  ...offboardingWidgetDefinitions,
  ...performanceWidgetDefinitions,
  ...talentWidgetDefinitions,
  ...workforceWidgetDefinitions,
  ...schedulingWidgetDefinitions,
  ...leaveWidgetDefinitions,
  ...benefitsWidgetDefinitions,
  ...payrollWidgetDefinitions,
  ...employeeRelationsWidgetDefinitions,
  ...complianceWidgetDefinitions,
  ...experienceWidgetDefinitions,
  ...coreHrisWidgetDefinitions,
  ...compensationAdvancedWidgetDefinitions,
  ...learningWidgetDefinitions,
  ...deiWidgetDefinitions,
  ...laborWidgetDefinitions,
  ...healthSafetyWidgetDefinitions,
  ...serviceDeliveryWidgetDefinitions,
  ...employeeFinanceWidgetDefinitions,
  ...globalMobilityWidgetDefinitions,
  ...privacyWidgetDefinitions,
  ...uiBlockWidgetDefinitions,
];

export const findWidgetDefinition = (
  registry: readonly WidgetDefinition[],
  widgetType: string,
): WidgetDefinition | undefined =>
  registry.find((definitionItem) => definitionItem.widgetType === widgetType);

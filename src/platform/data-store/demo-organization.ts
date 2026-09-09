import { ACTOR_ROLES } from "@human-capital-management-suite/foundation";
import type {
  AssignmentLifecycleStatus,
  EmployeeProjectionDocument,
  OrgLifecycleStatus,
  OrganizationRelationshipType,
  OrganizationUnitType,
  RoleBindingScopeType,
  WorkerAssignmentType,
} from "./types.js";

export const DEMO_ORGANIZATION = {
  name: "HarborCare Medical Group",
  slug: "harborcare-medical",
  legalEntity: "HarborCare Medical Group PC",
  industry: "multi-site primary care and outpatient behavioral health",
  isolationModel: "database_per_organization",
  recommendedDatabaseName: "hcm_next_harborcare",
  defaultTimezone: "America/New_York",
  defaultLocale: "en-US",
  dataRegion: "US",
} as const;

export const DEMO_EMPLOYEE_IDS = {
  SELF_SERVICE_EMPLOYEE: "emp_123",
  SECOND_HR_ADMIN: "emp_124",
  MANAGER: "emp_456",
  CAMBRIDGE_MANAGER: "emp_461",
  FINANCE_ADMIN: "emp_320",
  COMPENSATION_ADMIN: "emp_330",
  EXECUTIVE_DIRECTOR: "emp_910",
  MEDICAL_DIRECTOR: "emp_920",
  CLINIC_OPS_DIRECTOR: "emp_930",
  PEOPLE_DIRECTOR: "emp_940",
  FINANCE_CONTROLLER: "emp_950",
  COMPLIANCE_OFFICER: "emp_960",
  IT_MANAGER: "emp_980",
} as const;

export const DEMO_FINANCE_APPROVABLE_COST_CENTERS = [
  "FIN-200",
  "FIN-220",
  "REV-500",
  "REV-510",
  "REV-520",
  "CLN-BOS",
  "CLN-CAM",
] as const;

export type DemoAccessPersona =
  | "primary_hr_admin"
  | "secondary_hr_admin"
  | "finance_admin"
  | "compensation_admin"
  | "clinic_ops_admin"
  | "medical_director"
  | "compliance_admin"
  | "it_admin"
  | "executive";

export type DemoEmployeeSpec = {
  employeeId: string;
  personId: string;
  firstName: string;
  middleName?: string | null;
  lastName: string;
  preferredName?: string | null;
  title: string;
  family: string;
  level: string;
  department: string;
  team: string;
  businessUnit: string;
  location: DemoLocationKey;
  costCenter: string;
  jobCode: string;
  managerEmployeeId: string | null;
  hireDate: string;
  workerType: string;
  compensationAmount: number;
  bonusTargetPercent: number;
  roles?: readonly string[];
  accessPersonas?: readonly DemoAccessPersona[];
  emergencyContactName?: string;
};

const locationProfiles = {
  "Boston Main Clinic": {
    city: "Boston",
    region: "MA",
    postalCode: "02114",
    payZone: "US-EAST",
  },
  "Cambridge Clinic": {
    city: "Cambridge",
    region: "MA",
    postalCode: "02139",
    payZone: "US-EAST",
  },
  "Somerville Clinic": {
    city: "Somerville",
    region: "MA",
    postalCode: "02143",
    payZone: "US-EAST",
  },
  "Quincy Clinic": {
    city: "Quincy",
    region: "MA",
    postalCode: "02169",
    payZone: "US-EAST",
  },
  "HarborCare HQ": {
    city: "Boston",
    region: "MA",
    postalCode: "02110",
    payZone: "US-EAST",
  },
  Telehealth: {
    city: "Remote",
    region: "US",
    postalCode: "00000",
    payZone: "US-REMOTE",
  },
} as const;

export type DemoLocationKey = keyof typeof locationProfiles;

export const DEMO_ORG_TRANSFER_FIXTURE_ALIASES = {
  intent: "employee.org_transfer_compensation_change",
  sourceEmployeeId: DEMO_EMPLOYEE_IDS.SELF_SERVICE_EMPLOYEE,
  sourceManagerEmployeeId: DEMO_EMPLOYEE_IDS.MANAGER,
  sourceLocationName: "Boston Main Clinic",
  sourceTeamName: "Boston Nursing",
  sourceCostCenterName: "CLN-BOS",
  destinationManagerEmployeeId: DEMO_EMPLOYEE_IDS.CAMBRIDGE_MANAGER,
  destinationManagerActorId: "actor_emp_461",
  targetLocationName: "Cambridge Clinic",
  targetLocationOrgUnitKey: orgUnitKeyForLocation("Cambridge Clinic"),
  targetTeamName: "Cambridge Nursing",
  targetTeamOrgUnitKey: orgUnitKeyForTeam(
    "Clinical Operations",
    "Clinical Care",
    "Cambridge Nursing",
  ),
  targetCostCenterName: "CLN-CAM",
  targetCostCenterOrgUnitKey: orgUnitKeyForCostCenter("CLN-CAM"),
  targetManagerEmployeeId: DEMO_EMPLOYEE_IDS.CAMBRIDGE_MANAGER,
  proposedJob: {
    jobCode: "CLN-RN2",
    title: "Registered Nurse",
    family: "Nursing",
    level: "P2",
  },
  proposedCompensation: {
    amount: 98500,
    currency: "USD",
    payFrequency: "annual",
    bonusTargetPercent: 5,
    effectiveDate: "2026-05-01",
  },
  effectiveAt: "2026-05-01",
  businessReason: "operational_need",
  transferReason: "clinic_staffing_need",
} as const;

export const DEMO_EMPLOYEE_SPECS: readonly DemoEmployeeSpec[] = [
  {
    employeeId: DEMO_EMPLOYEE_IDS.EXECUTIVE_DIRECTOR,
    personId: "person_910",
    firstName: "Olivia",
    lastName: "Bennett",
    title: "Executive Director",
    family: "Executive",
    level: "E3",
    department: "Executive",
    team: "Executive Office",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "EXE-100",
    jobCode: "EXE-DIR-E3",
    managerEmployeeId: null,
    hireDate: "2018-02-05",
    workerType: "employee",
    compensationAmount: 245000,
    bonusTargetPercent: 30,
    roles: ["executive"],
    accessPersonas: ["executive"],
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.MEDICAL_DIRECTOR,
    personId: "person_920",
    firstName: "Daniel",
    lastName: "Cho",
    title: "Chief Medical Officer",
    family: "Clinical Leadership",
    level: "E2",
    department: "Clinical Care",
    team: "Medical Leadership",
    businessUnit: "Clinical Operations",
    location: "HarborCare HQ",
    costCenter: "CLN-100",
    jobCode: "CLN-CMO-E2",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.EXECUTIVE_DIRECTOR,
    hireDate: "2019-04-15",
    workerType: "employee",
    compensationAmount: 238000,
    bonusTargetPercent: 25,
    roles: ["clinical_admin"],
    accessPersonas: ["medical_director"],
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.CLINIC_OPS_DIRECTOR,
    personId: "person_930",
    firstName: "Priya",
    lastName: "Nair",
    title: "Director, Clinic Operations",
    family: "Operations Leadership",
    level: "M3",
    department: "Clinic Operations",
    team: "Clinic Operations",
    businessUnit: "Clinical Operations",
    location: "HarborCare HQ",
    costCenter: "OPS-100",
    jobCode: "OPS-DIR-M3",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.EXECUTIVE_DIRECTOR,
    hireDate: "2020-01-20",
    workerType: "employee",
    compensationAmount: 182000,
    bonusTargetPercent: 20,
    roles: ["clinic_ops_admin"],
    accessPersonas: ["clinic_ops_admin"],
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.PEOPLE_DIRECTOR,
    personId: "person_940",
    firstName: "Grace",
    lastName: "Kim",
    title: "Director, People Operations",
    family: "People",
    level: "M3",
    department: "People",
    team: "People Operations",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "PPL-100",
    jobCode: "PPL-DIR-M3",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.EXECUTIVE_DIRECTOR,
    hireDate: "2020-08-10",
    workerType: "employee",
    compensationAmount: 164000,
    bonusTargetPercent: 18,
    roles: [ACTOR_ROLES.HR_ADMIN],
    accessPersonas: ["primary_hr_admin"],
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.FINANCE_CONTROLLER,
    personId: "person_950",
    firstName: "Marcus",
    lastName: "Reed",
    title: "Controller",
    family: "Finance",
    level: "M3",
    department: "Finance",
    team: "Accounting",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "FIN-200",
    jobCode: "FIN-CTRL-M3",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.EXECUTIVE_DIRECTOR,
    hireDate: "2021-02-01",
    workerType: "employee",
    compensationAmount: 158000,
    bonusTargetPercent: 18,
    roles: [ACTOR_ROLES.FINANCE_ADMIN],
    accessPersonas: ["finance_admin"],
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.COMPLIANCE_OFFICER,
    personId: "person_960",
    firstName: "Nina",
    lastName: "Alvarez",
    title: "Compliance Officer",
    family: "Compliance",
    level: "P4",
    department: "Compliance",
    team: "Quality and Compliance",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "CMP-300",
    jobCode: "CMP-OFF-P4",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.EXECUTIVE_DIRECTOR,
    hireDate: "2021-07-12",
    workerType: "employee",
    compensationAmount: 142000,
    bonusTargetPercent: 12,
    roles: ["compliance_admin"],
    accessPersonas: ["compliance_admin"],
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.IT_MANAGER,
    personId: "person_980",
    firstName: "Leo",
    lastName: "Park",
    title: "IT Manager",
    family: "Information Technology",
    level: "M2",
    department: "IT",
    team: "Clinic Systems",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "IT-400",
    jobCode: "IT-MGR-M2",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.CLINIC_OPS_DIRECTOR,
    hireDate: "2021-06-14",
    workerType: "employee",
    compensationAmount: 136000,
    bonusTargetPercent: 12,
    roles: ["it_admin"],
    accessPersonas: ["it_admin"],
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.MANAGER,
    personId: "person_456",
    firstName: "Morgan",
    lastName: "Lee",
    title: "Clinic Manager, Boston",
    family: "Clinic Operations",
    level: "M1",
    department: "Clinic Operations",
    team: "Boston Main Clinic",
    businessUnit: "Clinical Operations",
    location: "Boston Main Clinic",
    costCenter: "CLN-BOS",
    jobCode: "OPS-CM-M1",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.CLINIC_OPS_DIRECTOR,
    hireDate: "2019-08-05",
    workerType: "employee",
    compensationAmount: 125000,
    bonusTargetPercent: 15,
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.CAMBRIDGE_MANAGER,
    personId: "person_461",
    firstName: "Sofia",
    lastName: "Rossi",
    title: "Clinic Manager, Cambridge",
    family: "Clinic Operations",
    level: "M1",
    department: "Clinic Operations",
    team: "Cambridge Clinic",
    businessUnit: "Clinical Operations",
    location: "Cambridge Clinic",
    costCenter: "CLN-CAM",
    jobCode: "OPS-CM-M1",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.CLINIC_OPS_DIRECTOR,
    hireDate: "2020-03-09",
    workerType: "employee",
    compensationAmount: 121000,
    bonusTargetPercent: 15,
  },
  {
    employeeId: "emp_462",
    personId: "person_462",
    firstName: "Jamal",
    lastName: "Carter",
    title: "Clinic Manager, Somerville",
    family: "Clinic Operations",
    level: "M1",
    department: "Clinic Operations",
    team: "Somerville Clinic",
    businessUnit: "Clinical Operations",
    location: "Somerville Clinic",
    costCenter: "CLN-SOM",
    jobCode: "OPS-CM-M1",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.CLINIC_OPS_DIRECTOR,
    hireDate: "2020-05-18",
    workerType: "employee",
    compensationAmount: 119000,
    bonusTargetPercent: 15,
  },
  {
    employeeId: "emp_463",
    personId: "person_463",
    firstName: "Hannah",
    lastName: "Walsh",
    title: "Clinic Manager, Quincy",
    family: "Clinic Operations",
    level: "M1",
    department: "Clinic Operations",
    team: "Quincy Clinic",
    businessUnit: "Clinical Operations",
    location: "Quincy Clinic",
    costCenter: "CLN-QUI",
    jobCode: "OPS-CM-M1",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.CLINIC_OPS_DIRECTOR,
    hireDate: "2020-11-02",
    workerType: "employee",
    compensationAmount: 118000,
    bonusTargetPercent: 15,
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.SECOND_HR_ADMIN,
    personId: "person_124",
    firstName: "Riley",
    middleName: "A.",
    lastName: "Patel",
    preferredName: "Riley",
    title: "People Operations Partner",
    family: "People",
    level: "P3",
    department: "People",
    team: "People Operations",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "PPL-100",
    jobCode: "PPL-HRB3",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.PEOPLE_DIRECTOR,
    hireDate: "2022-01-24",
    workerType: "employee",
    compensationAmount: 112000,
    bonusTargetPercent: 10,
    roles: [ACTOR_ROLES.HR_ADMIN, "hrbp"],
    accessPersonas: ["secondary_hr_admin"],
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.FINANCE_ADMIN,
    personId: "person_320",
    firstName: "Avery",
    lastName: "Chen",
    title: "Senior Accountant",
    family: "Finance",
    level: "P3",
    department: "Finance",
    team: "Accounting",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "FIN-200",
    jobCode: "FIN-ACCT3",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.FINANCE_CONTROLLER,
    hireDate: "2022-04-18",
    workerType: "employee",
    compensationAmount: 124000,
    bonusTargetPercent: 12,
    roles: [ACTOR_ROLES.FINANCE_ADMIN],
    accessPersonas: ["finance_admin"],
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.COMPENSATION_ADMIN,
    personId: "person_330",
    firstName: "Jordan",
    lastName: "Rivera",
    title: "Payroll and Compensation Manager",
    family: "People",
    level: "M1",
    department: "People",
    team: "Payroll and Compensation",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "PPL-120",
    jobCode: "PPL-COMP-M1",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.PEOPLE_DIRECTOR,
    hireDate: "2021-09-13",
    workerType: "employee",
    compensationAmount: 132000,
    bonusTargetPercent: 12,
    roles: [ACTOR_ROLES.COMPENSATION_ADMIN],
    accessPersonas: ["compensation_admin"],
  },
  {
    employeeId: DEMO_EMPLOYEE_IDS.SELF_SERVICE_EMPLOYEE,
    personId: "person_123",
    firstName: "Jane",
    lastName: "Doe",
    title: "Registered Nurse",
    family: "Nursing",
    level: "P2",
    department: "Clinical Care",
    team: "Boston Nursing",
    businessUnit: "Clinical Operations",
    location: "Boston Main Clinic",
    costCenter: "CLN-BOS",
    jobCode: "CLN-RN2",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.MANAGER,
    hireDate: "2021-03-15",
    workerType: "employee",
    compensationAmount: 93000,
    bonusTargetPercent: 5,
    emergencyContactName: "Alex Doe",
  },
  {
    employeeId: "emp_1001",
    personId: "person_1001",
    firstName: "Amara",
    lastName: "Okafor",
    title: "Family Physician",
    family: "Physician",
    level: "P5",
    department: "Clinical Care",
    team: "Boston Primary Care",
    businessUnit: "Clinical Operations",
    location: "Boston Main Clinic",
    costCenter: "CLN-BOS",
    jobCode: "CLN-FP5",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.MANAGER,
    hireDate: "2020-06-22",
    workerType: "employee",
    compensationAmount: 212000,
    bonusTargetPercent: 12,
  },
  {
    employeeId: "emp_1002",
    personId: "person_1002",
    firstName: "Noah",
    lastName: "Singh",
    title: "Nurse Practitioner",
    family: "Advanced Practice",
    level: "P4",
    department: "Clinical Care",
    team: "Boston Primary Care",
    businessUnit: "Clinical Operations",
    location: "Boston Main Clinic",
    costCenter: "CLN-BOS",
    jobCode: "CLN-NP4",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.MANAGER,
    hireDate: "2021-10-04",
    workerType: "employee",
    compensationAmount: 132000,
    bonusTargetPercent: 8,
  },
  {
    employeeId: "emp_1003",
    personId: "person_1003",
    firstName: "Mei",
    lastName: "Tan",
    title: "Medical Assistant",
    family: "Clinical Support",
    level: "P1",
    department: "Clinical Care",
    team: "Boston Clinical Support",
    businessUnit: "Clinical Operations",
    location: "Boston Main Clinic",
    costCenter: "CLN-BOS",
    jobCode: "CLN-MA1",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.MANAGER,
    hireDate: "2023-01-09",
    workerType: "employee",
    compensationAmount: 56000,
    bonusTargetPercent: 3,
  },
  {
    employeeId: "emp_1004",
    personId: "person_1004",
    firstName: "Lucas",
    lastName: "Rivera",
    title: "Front Desk Coordinator",
    family: "Patient Services",
    level: "P1",
    department: "Patient Services",
    team: "Boston Front Desk",
    businessUnit: "Clinical Operations",
    location: "Boston Main Clinic",
    costCenter: "CLN-BOS",
    jobCode: "PS-FDC1",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.MANAGER,
    hireDate: "2022-08-15",
    workerType: "employee",
    compensationAmount: 52000,
    bonusTargetPercent: 3,
  },
  {
    employeeId: "emp_1005",
    personId: "person_1005",
    firstName: "Chloe",
    lastName: "Martin",
    title: "Care Coordinator",
    family: "Care Coordination",
    level: "P2",
    department: "Care Coordination",
    team: "Boston Care Coordination",
    businessUnit: "Clinical Operations",
    location: "Boston Main Clinic",
    costCenter: "CLN-BOS",
    jobCode: "CARE-COORD2",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.MANAGER,
    hireDate: "2022-03-07",
    workerType: "employee",
    compensationAmount: 67000,
    bonusTargetPercent: 4,
  },
  {
    employeeId: "emp_1006",
    personId: "person_1006",
    firstName: "Ethan",
    lastName: "Brooks",
    title: "Billing Specialist",
    family: "Revenue Cycle",
    level: "P2",
    department: "Revenue Cycle",
    team: "Claims",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "REV-510",
    jobCode: "REV-BILL2",
    managerEmployeeId: "emp_1501",
    hireDate: "2022-05-23",
    workerType: "employee",
    compensationAmount: 64000,
    bonusTargetPercent: 4,
  },
  {
    employeeId: "emp_1101",
    personId: "person_1101",
    firstName: "Imani",
    lastName: "Johnson",
    title: "Family Physician",
    family: "Physician",
    level: "P5",
    department: "Clinical Care",
    team: "Cambridge Primary Care",
    businessUnit: "Clinical Operations",
    location: "Cambridge Clinic",
    costCenter: "CLN-CAM",
    jobCode: "CLN-FP5",
    managerEmployeeId: "emp_461",
    hireDate: "2020-09-14",
    workerType: "employee",
    compensationAmount: 209000,
    bonusTargetPercent: 12,
  },
  {
    employeeId: "emp_1102",
    personId: "person_1102",
    firstName: "Owen",
    lastName: "Patel",
    title: "Registered Nurse",
    family: "Nursing",
    level: "P2",
    department: "Clinical Care",
    team: "Cambridge Nursing",
    businessUnit: "Clinical Operations",
    location: "Cambridge Clinic",
    costCenter: "CLN-CAM",
    jobCode: "CLN-RN2",
    managerEmployeeId: "emp_461",
    hireDate: "2022-02-28",
    workerType: "employee",
    compensationAmount: 91000,
    bonusTargetPercent: 5,
  },
  {
    employeeId: "emp_1103",
    personId: "person_1103",
    firstName: "Violet",
    lastName: "Chen",
    title: "Medical Assistant",
    family: "Clinical Support",
    level: "P1",
    department: "Clinical Care",
    team: "Cambridge Clinical Support",
    businessUnit: "Clinical Operations",
    location: "Cambridge Clinic",
    costCenter: "CLN-CAM",
    jobCode: "CLN-MA1",
    managerEmployeeId: "emp_461",
    hireDate: "2023-04-10",
    workerType: "employee",
    compensationAmount: 55000,
    bonusTargetPercent: 3,
  },
  {
    employeeId: "emp_1104",
    personId: "person_1104",
    firstName: "Mateo",
    lastName: "Garcia",
    title: "Patient Services Representative",
    family: "Patient Services",
    level: "P1",
    department: "Patient Services",
    team: "Cambridge Front Desk",
    businessUnit: "Clinical Operations",
    location: "Cambridge Clinic",
    costCenter: "CLN-CAM",
    jobCode: "PS-REP1",
    managerEmployeeId: "emp_461",
    hireDate: "2022-10-17",
    workerType: "employee",
    compensationAmount: 50000,
    bonusTargetPercent: 3,
  },
  {
    employeeId: "emp_1105",
    personId: "person_1105",
    firstName: "Harper",
    lastName: "Wilson",
    title: "Care Coordinator",
    family: "Care Coordination",
    level: "P2",
    department: "Care Coordination",
    team: "Cambridge Care Coordination",
    businessUnit: "Clinical Operations",
    location: "Cambridge Clinic",
    costCenter: "CLN-CAM",
    jobCode: "CARE-COORD2",
    managerEmployeeId: "emp_461",
    hireDate: "2021-12-06",
    workerType: "employee",
    compensationAmount: 66000,
    bonusTargetPercent: 4,
  },
  {
    employeeId: "emp_1106",
    personId: "person_1106",
    firstName: "Aisha",
    lastName: "Khan",
    title: "Behavioral Health Clinician",
    family: "Behavioral Health",
    level: "P3",
    department: "Behavioral Health",
    team: "Cambridge Behavioral Health",
    businessUnit: "Clinical Operations",
    location: "Cambridge Clinic",
    costCenter: "CLN-CAM",
    jobCode: "BH-CLIN3",
    managerEmployeeId: "emp_461",
    hireDate: "2021-05-03",
    workerType: "employee",
    compensationAmount: 98000,
    bonusTargetPercent: 6,
  },
  {
    employeeId: "emp_1201",
    personId: "person_1201",
    firstName: "Samuel",
    lastName: "Green",
    title: "Family Physician",
    family: "Physician",
    level: "P5",
    department: "Clinical Care",
    team: "Somerville Primary Care",
    businessUnit: "Clinical Operations",
    location: "Somerville Clinic",
    costCenter: "CLN-SOM",
    jobCode: "CLN-FP5",
    managerEmployeeId: "emp_462",
    hireDate: "2019-09-30",
    workerType: "employee",
    compensationAmount: 211000,
    bonusTargetPercent: 12,
  },
  {
    employeeId: "emp_1202",
    personId: "person_1202",
    firstName: "Nora",
    lastName: "Lopez",
    title: "Nurse Practitioner",
    family: "Advanced Practice",
    level: "P4",
    department: "Clinical Care",
    team: "Somerville Primary Care",
    businessUnit: "Clinical Operations",
    location: "Somerville Clinic",
    costCenter: "CLN-SOM",
    jobCode: "CLN-NP4",
    managerEmployeeId: "emp_462",
    hireDate: "2020-12-07",
    workerType: "employee",
    compensationAmount: 129000,
    bonusTargetPercent: 8,
  },
  {
    employeeId: "emp_1203",
    personId: "person_1203",
    firstName: "Peter",
    lastName: "Novak",
    title: "Registered Nurse",
    family: "Nursing",
    level: "P2",
    department: "Clinical Care",
    team: "Somerville Nursing",
    businessUnit: "Clinical Operations",
    location: "Somerville Clinic",
    costCenter: "CLN-SOM",
    jobCode: "CLN-RN2",
    managerEmployeeId: "emp_462",
    hireDate: "2022-01-31",
    workerType: "employee",
    compensationAmount: 89500,
    bonusTargetPercent: 5,
  },
  {
    employeeId: "emp_1204",
    personId: "person_1204",
    firstName: "Leah",
    lastName: "Brown",
    title: "Medical Assistant",
    family: "Clinical Support",
    level: "P1",
    department: "Clinical Care",
    team: "Somerville Clinical Support",
    businessUnit: "Clinical Operations",
    location: "Somerville Clinic",
    costCenter: "CLN-SOM",
    jobCode: "CLN-MA1",
    managerEmployeeId: "emp_462",
    hireDate: "2023-03-20",
    workerType: "employee",
    compensationAmount: 54500,
    bonusTargetPercent: 3,
  },
  {
    employeeId: "emp_1205",
    personId: "person_1205",
    firstName: "Diana",
    lastName: "Flores",
    title: "Front Desk Coordinator",
    family: "Patient Services",
    level: "P1",
    department: "Patient Services",
    team: "Somerville Front Desk",
    businessUnit: "Clinical Operations",
    location: "Somerville Clinic",
    costCenter: "CLN-SOM",
    jobCode: "PS-FDC1",
    managerEmployeeId: "emp_462",
    hireDate: "2022-07-18",
    workerType: "employee",
    compensationAmount: 50500,
    bonusTargetPercent: 3,
  },
  {
    employeeId: "emp_1206",
    personId: "person_1206",
    firstName: "Ben",
    lastName: "Thomas",
    title: "Care Coordinator",
    family: "Care Coordination",
    level: "P2",
    department: "Care Coordination",
    team: "Somerville Care Coordination",
    businessUnit: "Clinical Operations",
    location: "Somerville Clinic",
    costCenter: "CLN-SOM",
    jobCode: "CARE-COORD2",
    managerEmployeeId: "emp_462",
    hireDate: "2021-11-15",
    workerType: "employee",
    compensationAmount: 65000,
    bonusTargetPercent: 4,
  },
  {
    employeeId: "emp_1301",
    personId: "person_1301",
    firstName: "Fatima",
    lastName: "Hassan",
    title: "Family Physician",
    family: "Physician",
    level: "P5",
    department: "Clinical Care",
    team: "Quincy Primary Care",
    businessUnit: "Clinical Operations",
    location: "Quincy Clinic",
    costCenter: "CLN-QUI",
    jobCode: "CLN-FP5",
    managerEmployeeId: "emp_463",
    hireDate: "2020-02-24",
    workerType: "employee",
    compensationAmount: 208000,
    bonusTargetPercent: 12,
  },
  {
    employeeId: "emp_1302",
    personId: "person_1302",
    firstName: "Aaron",
    lastName: "Kim",
    title: "Registered Nurse",
    family: "Nursing",
    level: "P2",
    department: "Clinical Care",
    team: "Quincy Nursing",
    businessUnit: "Clinical Operations",
    location: "Quincy Clinic",
    costCenter: "CLN-QUI",
    jobCode: "CLN-RN2",
    managerEmployeeId: "emp_463",
    hireDate: "2022-06-06",
    workerType: "employee",
    compensationAmount: 88500,
    bonusTargetPercent: 5,
  },
  {
    employeeId: "emp_1303",
    personId: "person_1303",
    firstName: "Julia",
    lastName: "Santos",
    title: "Medical Assistant",
    family: "Clinical Support",
    level: "P1",
    department: "Clinical Care",
    team: "Quincy Clinical Support",
    businessUnit: "Clinical Operations",
    location: "Quincy Clinic",
    costCenter: "CLN-QUI",
    jobCode: "CLN-MA1",
    managerEmployeeId: "emp_463",
    hireDate: "2023-02-13",
    workerType: "employee",
    compensationAmount: 54000,
    bonusTargetPercent: 3,
  },
  {
    employeeId: "emp_1304",
    personId: "person_1304",
    firstName: "Miles",
    lastName: "Davis",
    title: "Patient Services Representative",
    family: "Patient Services",
    level: "P1",
    department: "Patient Services",
    team: "Quincy Front Desk",
    businessUnit: "Clinical Operations",
    location: "Quincy Clinic",
    costCenter: "CLN-QUI",
    jobCode: "PS-REP1",
    managerEmployeeId: "emp_463",
    hireDate: "2022-09-19",
    workerType: "employee",
    compensationAmount: 50000,
    bonusTargetPercent: 3,
  },
  {
    employeeId: "emp_1305",
    personId: "person_1305",
    firstName: "Zoe",
    lastName: "Murphy",
    title: "Care Coordinator",
    family: "Care Coordination",
    level: "P2",
    department: "Care Coordination",
    team: "Quincy Care Coordination",
    businessUnit: "Clinical Operations",
    location: "Quincy Clinic",
    costCenter: "CLN-QUI",
    jobCode: "CARE-COORD2",
    managerEmployeeId: "emp_463",
    hireDate: "2021-08-16",
    workerType: "employee",
    compensationAmount: 65500,
    bonusTargetPercent: 4,
  },
  {
    employeeId: "emp_1306",
    personId: "person_1306",
    firstName: "Victor",
    lastName: "Chen",
    title: "Behavioral Health Clinician",
    family: "Behavioral Health",
    level: "P3",
    department: "Behavioral Health",
    team: "Quincy Behavioral Health",
    businessUnit: "Clinical Operations",
    location: "Quincy Clinic",
    costCenter: "CLN-QUI",
    jobCode: "BH-CLIN3",
    managerEmployeeId: "emp_463",
    hireDate: "2021-04-26",
    workerType: "employee",
    compensationAmount: 97000,
    bonusTargetPercent: 6,
  },
  {
    employeeId: "emp_1401",
    personId: "person_1401",
    firstName: "Sara",
    lastName: "Ahmed",
    title: "Telehealth Nurse",
    family: "Nursing",
    level: "P2",
    department: "Clinical Care",
    team: "Telehealth",
    businessUnit: "Clinical Operations",
    location: "Telehealth",
    costCenter: "CLN-TEL",
    jobCode: "TEL-RN2",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.CLINIC_OPS_DIRECTOR,
    hireDate: "2022-11-07",
    workerType: "employee",
    compensationAmount: 88000,
    bonusTargetPercent: 5,
  },
  {
    employeeId: "emp_1402",
    personId: "person_1402",
    firstName: "Kevin",
    lastName: "O'Brien",
    title: "Referral Coordinator",
    family: "Care Coordination",
    level: "P2",
    department: "Care Coordination",
    team: "Referrals",
    businessUnit: "Clinical Operations",
    location: "HarborCare HQ",
    costCenter: "CARE-610",
    jobCode: "CARE-REF2",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.CLINIC_OPS_DIRECTOR,
    hireDate: "2021-06-21",
    workerType: "employee",
    compensationAmount: 62000,
    bonusTargetPercent: 4,
  },
  {
    employeeId: "emp_1403",
    personId: "person_1403",
    firstName: "Priyanka",
    lastName: "Shah",
    title: "Quality Analyst",
    family: "Compliance",
    level: "P3",
    department: "Compliance",
    team: "Quality and Compliance",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "CMP-300",
    jobCode: "CMP-QA3",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.COMPLIANCE_OFFICER,
    hireDate: "2022-02-14",
    workerType: "employee",
    compensationAmount: 92000,
    bonusTargetPercent: 8,
  },
  {
    employeeId: "emp_1404",
    personId: "person_1404",
    firstName: "Tyler",
    lastName: "Moore",
    title: "Data Analyst",
    family: "Finance",
    level: "P2",
    department: "Finance",
    team: "Reporting",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "FIN-220",
    jobCode: "FIN-DA2",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.FINANCE_CONTROLLER,
    hireDate: "2022-09-06",
    workerType: "employee",
    compensationAmount: 88000,
    bonusTargetPercent: 8,
  },
  {
    employeeId: "emp_1405",
    personId: "person_1405",
    firstName: "Natalie",
    lastName: "Evans",
    title: "HR Coordinator",
    family: "People",
    level: "P1",
    department: "People",
    team: "People Operations",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "PPL-100",
    jobCode: "PPL-HRC1",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.PEOPLE_DIRECTOR,
    hireDate: "2023-01-17",
    workerType: "employee",
    compensationAmount: 64000,
    bonusTargetPercent: 5,
  },
  {
    employeeId: "emp_1406",
    personId: "person_1406",
    firstName: "Gabriel",
    lastName: "Young",
    title: "IT Support Specialist",
    family: "Information Technology",
    level: "P2",
    department: "IT",
    team: "Clinic Systems",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "IT-400",
    jobCode: "IT-SUP2",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.IT_MANAGER,
    hireDate: "2022-03-28",
    workerType: "employee",
    compensationAmount: 78000,
    bonusTargetPercent: 6,
  },
  {
    employeeId: "emp_1407",
    personId: "person_1407",
    firstName: "Lila",
    lastName: "Nguyen",
    title: "Credentialing Specialist",
    family: "Compliance",
    level: "P2",
    department: "Compliance",
    team: "Credentialing",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "CMP-310",
    jobCode: "CMP-CRED2",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.COMPLIANCE_OFFICER,
    hireDate: "2021-10-25",
    workerType: "employee",
    compensationAmount: 74000,
    bonusTargetPercent: 6,
  },
  {
    employeeId: "emp_1501",
    personId: "person_1501",
    firstName: "Brianna",
    lastName: "Scott",
    title: "Revenue Cycle Manager",
    family: "Revenue Cycle",
    level: "M1",
    department: "Revenue Cycle",
    team: "Revenue Cycle",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "REV-500",
    jobCode: "REV-MGR1",
    managerEmployeeId: DEMO_EMPLOYEE_IDS.FINANCE_CONTROLLER,
    hireDate: "2020-07-13",
    workerType: "employee",
    compensationAmount: 104000,
    bonusTargetPercent: 10,
  },
  {
    employeeId: "emp_1502",
    personId: "person_1502",
    firstName: "Omar",
    lastName: "Ali",
    title: "Claims Specialist",
    family: "Revenue Cycle",
    level: "P2",
    department: "Revenue Cycle",
    team: "Claims",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "REV-510",
    jobCode: "REV-CLAIM2",
    managerEmployeeId: "emp_1501",
    hireDate: "2022-08-08",
    workerType: "employee",
    compensationAmount: 61000,
    bonusTargetPercent: 4,
  },
  {
    employeeId: "emp_1503",
    personId: "person_1503",
    firstName: "Emily",
    lastName: "White",
    title: "Payment Posting Specialist",
    family: "Revenue Cycle",
    level: "P1",
    department: "Revenue Cycle",
    team: "Payment Posting",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "REV-520",
    jobCode: "REV-PAY1",
    managerEmployeeId: "emp_1501",
    hireDate: "2023-02-06",
    workerType: "employee",
    compensationAmount: 57000,
    bonusTargetPercent: 4,
  },
  {
    employeeId: "emp_1504",
    personId: "person_1504",
    firstName: "Malcolm",
    lastName: "Reed",
    title: "Patient Billing Specialist",
    family: "Revenue Cycle",
    level: "P2",
    department: "Revenue Cycle",
    team: "Patient Billing",
    businessUnit: "Corporate",
    location: "HarborCare HQ",
    costCenter: "REV-520",
    jobCode: "REV-BILL2",
    managerEmployeeId: "emp_1501",
    hireDate: "2023-05-22",
    workerType: "employee",
    compensationAmount: 60000,
    bonusTargetPercent: 4,
  },
];

export function createDemoEmployeeDocument(
  spec: DemoEmployeeSpec,
  index: number,
): EmployeeProjectionDocument {
  const location = locationProfiles[spec.location];
  const emailSlug = `${spec.firstName}.${spec.lastName}`
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, ".");

  return {
    employeeId: spec.employeeId,
    person: {
      personId: spec.personId,
      legalName: {
        first: spec.firstName,
        middle: spec.middleName ?? null,
        last: spec.lastName,
      },
      displayName: `${spec.firstName} ${spec.lastName}`,
      preferredName: spec.preferredName ?? null,
      workEmail: `${emailSlug}@harborcare.example`,
    },
    contact: {
      personalEmail: `${emailSlug}.personal@example.com`,
      mobilePhone: `+1555${String(2000000 + index).padStart(7, "0")}`,
      homeAddress: {
        line1: `${100 + index} Harbor Way`,
        line2: null,
        city: location.city,
        region: location.region,
        postalCode: location.postalCode,
        country: "US",
      },
    },
    employment: {
      status: "active",
      legalEntity: DEMO_ORGANIZATION.legalEntity,
      hireDate: spec.hireDate,
      workerType: spec.workerType,
    },
    organization: {
      legalEntity: DEMO_ORGANIZATION.legalEntity,
      businessUnit: spec.businessUnit,
      department: spec.department,
      team: spec.team,
      location: spec.location,
      payZone: location.payZone,
      costCenter: spec.costCenter,
    },
    manager: {
      employeeId: spec.managerEmployeeId,
    },
    job: {
      jobCode: spec.jobCode,
      title: spec.title,
      family: spec.family,
      level: spec.level,
    },
    compensation: {
      amount: spec.compensationAmount,
      currency: "USD",
      payFrequency: "annual",
      bonusTargetPercent: spec.bonusTargetPercent,
      effectiveDate: "2026-01-01",
    },
    emergencyContacts: [
      {
        contactId: `ec_${String(index + 1).padStart(3, "0")}`,
        name: spec.emergencyContactName ?? `${spec.firstName} ${spec.lastName} Sr.`,
        relationship: index % 3 === 0 ? "spouse" : "family",
        phone: `+1555${String(7000000 + index).padStart(7, "0")}`,
        email: null,
        priority: 1,
      },
    ],
    custom: {},
  };
}

export function employeeHasDirectReports(employeeId: string): boolean {
  return DEMO_EMPLOYEE_SPECS.some((spec) => {
    return spec.managerEmployeeId === employeeId;
  });
}

export function rolesForDemoEmployee(spec: DemoEmployeeSpec): string[] {
  const roles = new Set<string>([ACTOR_ROLES.EMPLOYEE]);

  if (employeeHasDirectReports(spec.employeeId)) {
    roles.add(ACTOR_ROLES.MANAGER);
  }

  for (const role of spec.roles ?? []) {
    roles.add(role);
  }

  return [...roles];
}

export function indexedFieldsForEmployeeDocument(
  document: EmployeeProjectionDocument,
): Record<string, unknown> {
  return {
    displayName: document.person.displayName,
    employmentStatus: document.employment.status,
    legalEntity: document.employment.legalEntity,
    businessUnit: document.organization.businessUnit,
    department: document.organization.department,
    team: document.organization.team,
    location: document.organization.location,
    costCenter: document.organization.costCenter,
    managerEmployeeId: document.manager.employeeId,
    jobCode: document.job.jobCode,
    jobLevel: document.job.level,
  };
}

export type DemoOrgUnitSpec = {
  unitKey: string;
  type: OrganizationUnitType;
  name: string;
  status: OrgLifecycleStatus;
  parentUnitKey?: string | undefined;
  relationshipToParent?: OrganizationRelationshipType | undefined;
  country?: string | undefined;
  jurisdiction?: string | undefined;
  metadata: Record<string, unknown>;
};

export type DemoWorkerAssignmentSpec = {
  assignmentKey: string;
  employeeId: string;
  orgUnitKey: string;
  assignmentType: WorkerAssignmentType;
  roleType?: string | undefined;
  managerEmployeeId?: string | undefined;
  allocationPercent: number;
  status: AssignmentLifecycleStatus;
  effectiveStart: string;
  effectiveEnd?: string | undefined;
  metadata: Record<string, unknown>;
};

export type DemoRoleBindingSpec = {
  bindingKey: string;
  employeeId: string;
  roleKey: string;
  scopeType: RoleBindingScopeType;
  scopeOrgUnitKey?: string | undefined;
  scopeValue?: string | undefined;
  relationshipType?: string | undefined;
  status: AssignmentLifecycleStatus;
  effectiveStart: string;
  effectiveEnd?: string | undefined;
  metadata: Record<string, unknown>;
};

export const DEMO_ORG_UNIT_KEYS = {
  ENTERPRISE: "enterprise_harborcare",
  LEGAL_ENTITY: "legal_entity_harborcare_pc",
  CORPORATE: "business_unit_corporate",
  CLINICAL_OPERATIONS: "business_unit_clinical_operations",
} as const;

export const DEMO_ORG_UNIT_SPECS: readonly DemoOrgUnitSpec[] = createDemoOrgUnitSpecs();

export const DEMO_WORKER_ASSIGNMENT_SPECS: readonly DemoWorkerAssignmentSpec[] =
  DEMO_EMPLOYEE_SPECS.flatMap((spec) => {
    return createDemoWorkerAssignmentSpecs(spec);
  });

export const DEMO_ROLE_BINDING_SPECS: readonly DemoRoleBindingSpec[] =
  createDemoRoleBindingSpecs();

export function orgUnitKeyForBusinessUnit(businessUnit: string): string {
  if (businessUnit === "Corporate") {
    return DEMO_ORG_UNIT_KEYS.CORPORATE;
  }

  if (businessUnit === "Clinical Operations") {
    return DEMO_ORG_UNIT_KEYS.CLINICAL_OPERATIONS;
  }

  return orgUnitKey("business_unit", businessUnit);
}

export function orgUnitKeyForDepartment(
  businessUnit: string,
  department: string,
): string {
  return orgUnitKey("department", `${businessUnit}_${department}`);
}

export function orgUnitKeyForTeam(
  businessUnit: string,
  department: string,
  team: string,
): string {
  return orgUnitKey("team", `${businessUnit}_${department}_${team}`);
}

export function orgUnitKeyForLocation(location: string): string {
  return orgUnitKey("location", location);
}

export function orgUnitKeyForCostCenter(costCenter: string): string {
  return orgUnitKey("cost_center", costCenter);
}

function createDemoOrgUnitSpecs(): DemoOrgUnitSpec[] {
  const orgUnits: DemoOrgUnitSpec[] = [
    {
      unitKey: DEMO_ORG_UNIT_KEYS.ENTERPRISE,
      type: "enterprise",
      name: DEMO_ORGANIZATION.name,
      status: "active",
      country: DEMO_ORGANIZATION.dataRegion,
      jurisdiction: "US",
      metadata: {
        demo: true,
        slug: DEMO_ORGANIZATION.slug,
        industry: DEMO_ORGANIZATION.industry,
        isolationModel: DEMO_ORGANIZATION.isolationModel,
      },
    },
    {
      unitKey: DEMO_ORG_UNIT_KEYS.LEGAL_ENTITY,
      type: "legal_entity",
      name: DEMO_ORGANIZATION.legalEntity,
      status: "active",
      parentUnitKey: DEMO_ORG_UNIT_KEYS.ENTERPRISE,
      relationshipToParent: "part_of",
      country: DEMO_ORGANIZATION.dataRegion,
      jurisdiction: "US-MA",
      metadata: {
        demo: true,
        legalEmployer: true,
      },
    },
    {
      unitKey: DEMO_ORG_UNIT_KEYS.CORPORATE,
      type: "business_unit",
      name: "Corporate",
      status: "active",
      parentUnitKey: DEMO_ORG_UNIT_KEYS.ENTERPRISE,
      relationshipToParent: "part_of",
      country: DEMO_ORGANIZATION.dataRegion,
      jurisdiction: "US",
      metadata: { demo: true },
    },
    {
      unitKey: DEMO_ORG_UNIT_KEYS.CLINICAL_OPERATIONS,
      type: "business_unit",
      name: "Clinical Operations",
      status: "active",
      parentUnitKey: DEMO_ORG_UNIT_KEYS.ENTERPRISE,
      relationshipToParent: "part_of",
      country: DEMO_ORGANIZATION.dataRegion,
      jurisdiction: "US-MA",
      metadata: { demo: true, regulatedCareDelivery: true },
    },
  ];

  for (const departmentSpec of createDepartmentOrgUnitSpecs()) {
    orgUnits.push(departmentSpec);
  }

  for (const teamSpec of createTeamOrgUnitSpecs()) {
    orgUnits.push(teamSpec);
  }

  for (const locationSpec of createLocationOrgUnitSpecs()) {
    orgUnits.push(locationSpec);
  }

  for (const costCenterSpec of createCostCenterOrgUnitSpecs()) {
    orgUnits.push(costCenterSpec);
  }

  return orgUnits;
}

function createDepartmentOrgUnitSpecs(): DemoOrgUnitSpec[] {
  return uniquePairs(DEMO_EMPLOYEE_SPECS, (spec) => {
    return `${spec.businessUnit}::${spec.department}`;
  }).map((spec) => {
    return {
      unitKey: orgUnitKeyForDepartment(spec.businessUnit, spec.department),
      type: "department",
      name: spec.department,
      status: "active",
      parentUnitKey: orgUnitKeyForBusinessUnit(spec.businessUnit),
      relationshipToParent: "part_of",
      country: DEMO_ORGANIZATION.dataRegion,
      jurisdiction: spec.businessUnit === "Clinical Operations" ? "US-MA" : "US",
      metadata: {
        demo: true,
        businessUnit: spec.businessUnit,
      },
    };
  });
}

function createTeamOrgUnitSpecs(): DemoOrgUnitSpec[] {
  return uniquePairs(DEMO_EMPLOYEE_SPECS, (spec) => {
    return `${spec.businessUnit}::${spec.department}::${spec.team}`;
  }).map((spec) => {
    return {
      unitKey: orgUnitKeyForTeam(spec.businessUnit, spec.department, spec.team),
      type: "team",
      name: spec.team,
      status: "active",
      parentUnitKey: orgUnitKeyForDepartment(spec.businessUnit, spec.department),
      relationshipToParent: "part_of",
      country: DEMO_ORGANIZATION.dataRegion,
      jurisdiction: spec.businessUnit === "Clinical Operations" ? "US-MA" : "US",
      metadata: {
        demo: true,
        businessUnit: spec.businessUnit,
        department: spec.department,
      },
    };
  });
}

function createLocationOrgUnitSpecs(): DemoOrgUnitSpec[] {
  return uniquePairs(DEMO_EMPLOYEE_SPECS, (spec) => spec.location).map((spec) => {
    const location = locationProfiles[spec.location];

    return {
      unitKey: orgUnitKeyForLocation(spec.location),
      type: "location",
      name: spec.location,
      status: "active",
      parentUnitKey: DEMO_ORG_UNIT_KEYS.LEGAL_ENTITY,
      relationshipToParent: "located_in",
      country: "US",
      jurisdiction: `US-${location.region}`,
      metadata: {
        demo: true,
        city: location.city,
        region: location.region,
        postalCode: location.postalCode,
        payZone: location.payZone,
      },
    };
  });
}

function createCostCenterOrgUnitSpecs(): DemoOrgUnitSpec[] {
  return uniquePairs(DEMO_EMPLOYEE_SPECS, (spec) => spec.costCenter).map((spec) => {
    return {
      unitKey: orgUnitKeyForCostCenter(spec.costCenter),
      type: "cost_center",
      name: spec.costCenter,
      status: "active",
      parentUnitKey: orgUnitKeyForBusinessUnit(spec.businessUnit),
      relationshipToParent: "allocated_to",
      country: DEMO_ORGANIZATION.dataRegion,
      jurisdiction: "US",
      metadata: {
        demo: true,
        businessUnit: spec.businessUnit,
        department: spec.department,
      },
    };
  });
}

function createDemoWorkerAssignmentSpecs(
  spec: DemoEmployeeSpec,
): DemoWorkerAssignmentSpec[] {
  const commonMetadata = {
    demo: true,
    seededFromProjection: true,
    tighteningWorkflowReady: true,
  };

  return [
    {
      assignmentKey: `assignment_${spec.employeeId}_legal_employer`,
      employeeId: spec.employeeId,
      orgUnitKey: DEMO_ORG_UNIT_KEYS.LEGAL_ENTITY,
      assignmentType: "legal_employer",
      roleType: spec.workerType,
      allocationPercent: 100,
      status: "active",
      effectiveStart: `${spec.hireDate}T00:00:00.000Z`,
      metadata: {
        ...commonMetadata,
        legalEntity: DEMO_ORGANIZATION.legalEntity,
      },
    },
    {
      assignmentKey: `assignment_${spec.employeeId}_primary_team`,
      employeeId: spec.employeeId,
      orgUnitKey: orgUnitKeyForTeam(spec.businessUnit, spec.department, spec.team),
      assignmentType: "primary_team",
      roleType: spec.title,
      managerEmployeeId: spec.managerEmployeeId ?? undefined,
      allocationPercent: 100,
      status: "active",
      effectiveStart: `${spec.hireDate}T00:00:00.000Z`,
      metadata: {
        ...commonMetadata,
        businessUnit: spec.businessUnit,
        department: spec.department,
        team: spec.team,
      },
    },
    {
      assignmentKey: `assignment_${spec.employeeId}_work_location`,
      employeeId: spec.employeeId,
      orgUnitKey: orgUnitKeyForLocation(spec.location),
      assignmentType: "work_location",
      allocationPercent: 100,
      status: "active",
      effectiveStart: `${spec.hireDate}T00:00:00.000Z`,
      metadata: {
        ...commonMetadata,
        location: spec.location,
      },
    },
    {
      assignmentKey: `assignment_${spec.employeeId}_cost_center`,
      employeeId: spec.employeeId,
      orgUnitKey: orgUnitKeyForCostCenter(spec.costCenter),
      assignmentType: "cost_center",
      allocationPercent: 100,
      status: "active",
      effectiveStart: `${spec.hireDate}T00:00:00.000Z`,
      metadata: {
        ...commonMetadata,
        costCenter: spec.costCenter,
      },
    },
  ];
}

function createDemoRoleBindingSpecs(): DemoRoleBindingSpec[] {
  const roleBindings: DemoRoleBindingSpec[] = [];

  for (const spec of DEMO_EMPLOYEE_SPECS) {
    roleBindings.push({
      bindingKey: `role_binding_${spec.employeeId}_employee_self`,
      employeeId: spec.employeeId,
      roleKey: ACTOR_ROLES.EMPLOYEE,
      scopeType: "self",
      relationshipType: "own_worker_record",
      status: "active",
      effectiveStart: `${spec.hireDate}T00:00:00.000Z`,
      metadata: {
        demo: true,
        accessProfile: "employee_self_service",
        fieldGroups: ["profile", "employment", "contact", "emergency_contacts"],
      },
    });

    if (employeeHasDirectReports(spec.employeeId)) {
      roleBindings.push({
        bindingKey: `role_binding_${spec.employeeId}_manager_direct_reports`,
        employeeId: spec.employeeId,
        roleKey: ACTOR_ROLES.MANAGER,
        scopeType: "direct_reports",
        relationshipType: "solid_line_manager",
        status: "active",
        effectiveStart: `${spec.hireDate}T00:00:00.000Z`,
        metadata: {
          demo: true,
          accessProfile: "manager_direct_reports",
          fieldGroups: ["profile", "organization", "job", "employment"],
        },
      });
    }

    roleBindings.push(...personaRoleBindingSpecs(spec));
  }

  return roleBindings;
}

function personaRoleBindingSpecs(spec: DemoEmployeeSpec): DemoRoleBindingSpec[] {
  const roleBindings: DemoRoleBindingSpec[] = [];
  const effectiveStart = `${spec.hireDate}T00:00:00.000Z`;

  for (const persona of spec.accessPersonas ?? []) {
    if (persona === "primary_hr_admin") {
      roleBindings.push(
        personaRoleBinding(spec, persona, ACTOR_ROLES.HR_ADMIN, "global", {
          effectiveStart,
          fieldGroups: [
            "profile",
            "organization",
            "job",
            "employment",
            "contact",
            "emergency_contacts",
            "workflow",
          ],
        }),
      );
      continue;
    }

    if (persona === "secondary_hr_admin") {
      for (const orgUnitKey of [
        DEMO_ORG_UNIT_KEYS.CLINICAL_OPERATIONS,
        DEMO_ORG_UNIT_KEYS.CORPORATE,
      ]) {
        roleBindings.push(
          personaRoleBinding(
            spec,
            persona,
            ACTOR_ROLES.HR_ADMIN,
            "org_unit_descendants",
            {
              effectiveStart,
              orgUnitKey,
              fieldGroups: [
                "profile",
                "organization",
                "job",
                "employment",
                "contact",
                "emergency_contacts",
                "workflow",
              ],
            },
          ),
        );
      }
      continue;
    }

    if (persona === "finance_admin") {
      for (const costCenter of DEMO_FINANCE_APPROVABLE_COST_CENTERS) {
        roleBindings.push(
          personaRoleBinding(spec, persona, ACTOR_ROLES.FINANCE_ADMIN, "cost_center", {
            effectiveStart,
            orgUnitKey: orgUnitKeyForCostCenter(costCenter),
            scopeValue: costCenter,
            fieldGroups: [
              "profile",
              "organization",
              "job",
              "employment",
              "compensation",
            ],
          }),
        );
      }
      continue;
    }

    if (persona === "compensation_admin") {
      roleBindings.push(
        personaRoleBinding(spec, persona, ACTOR_ROLES.COMPENSATION_ADMIN, "global", {
          effectiveStart,
          fieldGroups: ["profile", "organization", "job", "employment", "compensation"],
        }),
      );
      continue;
    }

    if (persona === "clinic_ops_admin") {
      roleBindings.push(
        personaRoleBinding(spec, persona, "clinic_ops_admin", "org_unit_descendants", {
          effectiveStart,
          orgUnitKey: DEMO_ORG_UNIT_KEYS.CLINICAL_OPERATIONS,
          fieldGroups: ["profile", "organization", "job", "employment"],
        }),
      );
      continue;
    }

    if (persona === "medical_director") {
      for (const department of [
        "Clinical Care",
        "Behavioral Health",
        "Care Coordination",
      ]) {
        roleBindings.push(
          personaRoleBinding(spec, persona, "clinical_admin", "org_unit_descendants", {
            effectiveStart,
            orgUnitKey: orgUnitKeyForDepartment("Clinical Operations", department),
            fieldGroups: ["profile", "organization", "job", "employment"],
          }),
        );
      }
      continue;
    }

    if (persona === "compliance_admin") {
      roleBindings.push(
        personaRoleBinding(spec, persona, "compliance_admin", "global", {
          effectiveStart,
          fieldGroups: ["profile", "organization", "job", "employment", "workflow"],
        }),
      );
      continue;
    }

    if (persona === "it_admin") {
      roleBindings.push(
        personaRoleBinding(spec, persona, "it_admin", "global", {
          effectiveStart,
          fieldGroups: ["profile", "organization", "job"],
        }),
      );
      continue;
    }

    if (persona === "executive") {
      roleBindings.push(
        personaRoleBinding(spec, persona, "executive", "global", {
          effectiveStart,
          fieldGroups: ["profile", "organization", "job", "employment", "compensation"],
        }),
      );
    }
  }

  return roleBindings;
}

function personaRoleBinding(
  spec: DemoEmployeeSpec,
  persona: DemoAccessPersona,
  roleKey: string,
  scopeType: RoleBindingScopeType,
  input: {
    effectiveStart: string;
    orgUnitKey?: string | undefined;
    scopeValue?: string | undefined;
    fieldGroups: readonly string[];
  },
): DemoRoleBindingSpec {
  const scopedSuffix = input.orgUnitKey ?? input.scopeValue ?? scopeType;

  return {
    bindingKey: `role_binding_${spec.employeeId}_${persona}_${scopedSuffix}`,
    employeeId: spec.employeeId,
    roleKey,
    scopeType,
    scopeOrgUnitKey: input.orgUnitKey,
    scopeValue: input.scopeValue,
    relationshipType: persona,
    status: "active",
    effectiveStart: input.effectiveStart,
    metadata: {
      demo: true,
      accessProfile: persona,
      fieldGroups: [...input.fieldGroups],
    },
  };
}

function uniquePairs<TItem>(
  items: readonly TItem[],
  keyForItem: (item: TItem) => string,
): TItem[] {
  const seenKeys = new Set<string>();
  const uniqueItems: TItem[] = [];

  for (const item of items) {
    const key = keyForItem(item);

    if (seenKeys.has(key)) {
      continue;
    }

    seenKeys.add(key);
    uniqueItems.push(item);
  }

  return uniqueItems;
}

function orgUnitKey(prefix: string, value: string): string {
  return `${prefix}_${normalizeOrgKey(value)}`;
}

function normalizeOrgKey(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
}

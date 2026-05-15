export const CONFIG_DEFAULTS = {
  DATABASE_URL: "postgres://postgres:postgres@localhost:5432/hcm_next",
  WEB_PORT: 3000,
  GO_EXECUTOR_PORT: 7001,
} as const;

export const DEMO_SEED_ALIASES = {
  TENANT: "tenant_demo",
  ENVIRONMENT: "env_demo",
  EMPLOYEE_ACTOR: "actor_employee_jane",
  HR_ACTOR: "actor_hr_admin",
  SYSTEM_ACTOR: "actor_system",
  EMPLOYEE: "emp_123",
  PERSON: "person_123",
  MANAGER_EMPLOYEE: "emp_456",
} as const;

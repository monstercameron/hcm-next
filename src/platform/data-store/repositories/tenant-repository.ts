import type { Result } from "@human-capital-management-suite/foundation";

import type { DatabaseClient } from "../client";
import { findOne } from "./repository-utils";
import type { TenantRecord } from "./repository-types";

export type TenantRepository = {
  /** Finds a tenant by primary identifier. */
  findById: (tenantId: string) => Promise<Result<TenantRecord>>;
  /** Finds a tenant by stable slug. */
  findBySlug: (slug: string) => Promise<Result<TenantRecord>>;
};

/**
 * Creates repository methods for tenant reads.
 */
export function createTenantRepository(database: DatabaseClient): TenantRepository {
  return {
    findById(tenantId) {
      return findOne<TenantRecord>(
        database,
        "SELECT * FROM tenants WHERE tenant_id = $1",
        [tenantId],
        "Tenant",
        { tenantId },
      );
    },
    findBySlug(slug) {
      return findOne<TenantRecord>(
        database,
        "SELECT * FROM tenants WHERE slug = $1",
        [slug],
        "Tenant",
        { slug },
      );
    },
  };
}

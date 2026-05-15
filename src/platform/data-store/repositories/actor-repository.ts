import type { Result } from "@hcm-next/foundation";

import type { DatabaseClient } from "../client";
import { findOne } from "./repository-utils";
import type { ActorRecord } from "./repository-types";

export type ActorRepository = {
  /** Finds an actor by primary identifier. */
  findById: (tenantId: string, actorId: string) => Promise<Result<ActorRecord>>;
  /** Finds an actor by demo or identity-provider subject. */
  findByExternalSubject: (
    tenantId: string,
    externalSubject: string,
  ) => Promise<Result<ActorRecord>>;
};

/**
 * Creates repository methods for actor reads.
 */
export function createActorRepository(database: DatabaseClient): ActorRepository {
  return {
    findById(tenantId, actorId) {
      return findOne<ActorRecord>(
        database,
        "SELECT * FROM actors WHERE tenant_id = $1 AND actor_id = $2",
        [tenantId, actorId],
        "Actor",
        { tenantId, actorId },
      );
    },
    findByExternalSubject(tenantId, externalSubject) {
      return findOne<ActorRecord>(
        database,
        "SELECT * FROM actors WHERE tenant_id = $1 AND external_subject = $2",
        [tenantId, externalSubject],
        "Actor",
        { tenantId, externalSubject },
      );
    },
  };
}

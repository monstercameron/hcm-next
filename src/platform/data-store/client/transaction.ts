import type { Pool, PoolClient } from "pg";

import {
  err,
  fromPromise,
  mapUnknownToDatabaseError,
  ok,
  type Result,
} from "@hcm-next/foundation";

/**
 * Executes a checked operation inside an explicit Postgres transaction.
 */
export async function executeInTransaction<TValue>(
  pool: Pool,
  operation: (client: PoolClient) => Promise<Result<TValue>>,
): Promise<Result<TValue>> {
  const clientResult = await fromPromise(
    async () => pool.connect(),
    mapUnknownToDatabaseError,
  );

  if (!clientResult.ok) {
    return clientResult;
  }

  const client = clientResult.value;

  try {
    await client.query("BEGIN");

    const operationResult = await operation(client);

    if (!operationResult.ok) {
      await client.query("ROLLBACK");

      return operationResult;
    }

    await client.query("COMMIT");

    return ok(operationResult.value);
  } catch (error) {
    await client.query("ROLLBACK");

    return err(mapUnknownToDatabaseError(error));
  } finally {
    client.release();
  }
}

import {
  err,
  ok,
  validationFailedError,
  type AppError,
  type Result,
} from "@human-capital-management-suite/foundation";
import type { EmployeeProjectionDocument } from "@human-capital-management-suite/data-store";
import { cloneJsonValue, stringField } from "./json-fields.js";

type ProjectionPatch = {
  projection: string;
  operation: string;
  path: string;
  value: unknown;
};

/**
 * Applies validated JSON-pointer patches to an employee projection copy.
 */
export function applyProjectionPatches(input: {
  document: EmployeeProjectionDocument;
  patches: Record<string, unknown>[];
  allowedPatchPaths: string[];
}): Result<EmployeeProjectionDocument, AppError> {
  if (input.patches.length === 0) {
    return err(validationFailedError({ projectionPatches: "missing" }));
  }

  const updatedDocument = cloneJsonValue(input.document);

  for (const patch of input.patches) {
    const patchResult = parseProjectionPatch(patch, input.allowedPatchPaths);
    if (!patchResult.ok) {
      return patchResult;
    }

    const applyResult = setJsonPointerValue(updatedDocument, patchResult.value);
    if (!applyResult.ok) {
      return applyResult;
    }
  }

  return ok(updatedDocument);
}

function parseProjectionPatch(
  patch: Record<string, unknown>,
  allowedPatchPaths: string[],
): Result<ProjectionPatch, AppError> {
  const projection = stringField(patch, "projection");
  const operation = stringField(patch, "operation");
  const path = stringField(patch, "path");

  if (
    projection !== "employee" ||
    operation !== "replace" ||
    path === undefined ||
    !allowedPatchPaths.includes(path)
  ) {
    return err(
      validationFailedError({
        projection,
        operation,
        path,
        allowedPatchPaths,
      }),
    );
  }

  return ok({
    projection,
    operation,
    path,
    value: patch["value"],
  });
}

function setJsonPointerValue(
  document: EmployeeProjectionDocument,
  patch: Pick<ProjectionPatch, "path" | "value">,
): Result<true, AppError> {
  const pathSegmentsResult = parseJsonPointerSegments(patch.path);
  if (!pathSegmentsResult.ok) {
    return pathSegmentsResult;
  }

  const pathSegments = pathSegmentsResult.value;
  const targetKey = pathSegments.at(-1);

  if (targetKey === undefined) {
    return err(validationFailedError({ path: patch.path }));
  }

  let parentValue: unknown = document;

  for (const pathSegment of pathSegments.slice(0, -1)) {
    if (typeof parentValue !== "object" || parentValue === null) {
      return err(validationFailedError({ path: patch.path, pathSegment }));
    }

    parentValue = (parentValue as Record<string, unknown>)[pathSegment];
  }

  if (typeof parentValue !== "object" || parentValue === null) {
    return err(validationFailedError({ path: patch.path, targetKey }));
  }

  (parentValue as Record<string, unknown>)[targetKey] = cloneJsonValue(patch.value);

  return ok(true);
}

function parseJsonPointerSegments(path: string): Result<string[], AppError> {
  if (!path.startsWith("/")) {
    return err(validationFailedError({ path }));
  }

  const pathSegments = path
    .slice(1)
    .split("/")
    .filter((segment) => segment.length > 0)
    .map((segment) => {
      return segment.replace(/~1/g, "/").replace(/~0/g, "~");
    });

  if (pathSegments.length === 0) {
    return err(validationFailedError({ path }));
  }

  return ok(pathSegments);
}

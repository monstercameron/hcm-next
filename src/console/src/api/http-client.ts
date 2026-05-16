import { systemError } from "@hcm-next/foundation";

export type FetchLike = typeof fetch;

export const browserFetch: FetchLike = (...args) => fetch(...args);

export const browserFetchOrReject: FetchLike = (...args) =>
  typeof fetch === "function"
    ? fetch(...args)
    : Promise.reject(systemError({ reason: "fetch_unavailable" }));

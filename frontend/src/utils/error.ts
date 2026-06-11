// getErrorMessage extracts a human-readable message from an unknown thrown
// value. Using `unknown` in catch clauses (instead of `any`) keeps type safety
// while still handling non-Error throwables gracefully.
export function getErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === 'string') {
    return error;
  }
  return String(error);
}

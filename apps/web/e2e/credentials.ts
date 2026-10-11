import type { Page, APIRequestContext } from "@playwright/test";

// Playwright's fill failure call log contains the value even for password inputs.
// Never forward that error, its message, or its cause into test output.
export async function enterAccessToken(
  page: Page,
  token: string,
  timeout = 5000,
) {
  try {
    await page.getByLabel("API access token").fill(token, { timeout });
  } catch {
    throw new Error(
      "Could not enter the test access token; check the login control.",
    );
  }
}

export function redactedRequests(request: APIRequestContext) {
  return {
    async post(...args: Parameters<APIRequestContext["post"]>) {
      try {
        return await request.post(...args);
      } catch {
        throw new Error(
          "Authenticated fixture request failed; check the service.",
        );
      }
    },
  };
}

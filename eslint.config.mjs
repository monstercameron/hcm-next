import js from "@eslint/js";
import prettier from "eslint-config-prettier";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: [
      // Repository-owned disposable output, including isolated worktrees.
      ".artifacts/**",
      "**/dist/**",
      "**/.next/**",
      "**/coverage/**",
      "**/node_modules/**",
      "**/next-env.d.ts",
      // Planning design demo: a static mock-up and its Node test harness, not
      // application code.
      "planning/design/demo/**",
      // Vendored Go toolchain shim (gitignored, copied from GOROOT/lib/wasm).
      "internal/humanwork/workspace/assets/wasm_exec.js",
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  prettier,
  {
    languageOptions: {
      globals: {
        __dirname: "readonly",
        document: "readonly",
        fetch: "readonly",
        globalThis: "readonly",
        Headers: "readonly",
        localStorage: "readonly",
        process: "readonly",
        Request: "readonly",
        Response: "readonly",
        window: "readonly",
      },
    },
  },
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      "@typescript-eslint/consistent-type-imports": "off",
      "@typescript-eslint/no-floating-promises": "error",
      "@typescript-eslint/no-misused-promises": "error",
      "@typescript-eslint/no-unsafe-argument": "off",
      "@typescript-eslint/no-unsafe-assignment": "off",
      "@typescript-eslint/no-unsafe-call": "off",
      "@typescript-eslint/no-unsafe-member-access": "off",
      "@typescript-eslint/no-unsafe-return": "off",
      "no-console": "error",
      "no-restricted-syntax": [
        "error",
        {
          selector: "TryStatement",
          message:
            "Use Result wrappers such as fromPromise/fromThrowable outside approved boundary modules.",
        },
      ],
    },
  },
  {
    files: [
      "src/platform/foundation/result/from-promise.ts",
      "src/platform/foundation/result/from-throwable.ts",
      "src/platform/data-store/client/transaction.ts",
      "src/platform/client-device-state/device-state.ts",
    ],
    rules: {
      "no-restricted-syntax": "off",
    },
  },
);

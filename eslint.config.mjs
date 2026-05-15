import js from "@eslint/js";
import prettier from "eslint-config-prettier";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: [
      "**/dist/**",
      "**/.next/**",
      "**/coverage/**",
      "**/node_modules/**",
      "**/next-env.d.ts",
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  prettier,
  {
    languageOptions: {
      globals: {
        __dirname: "readonly",
        globalThis: "readonly",
        process: "readonly",
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
    ],
    rules: {
      "no-restricted-syntax": "off",
    },
  },
);

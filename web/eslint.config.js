import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    // dist is the build output, schema.d.ts is generated from api/openapi.yaml.
    ignores: ["dist/", "src/api/schema.d.ts"],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2022,
      globals: { ...globals.browser },
    },
    plugins: {
      "react-hooks": reactHooks,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // TypeScript already checks identifiers, no-undef only produces false
      // positives for types and JSX.
      "no-undef": "off",
    },
  },
  {
    files: ["vite.config.ts", "eslint.config.js"],
    languageOptions: {
      globals: { ...globals.node },
    },
  },
);

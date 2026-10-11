import tsParser from "@typescript-eslint/parser";
import tsPlugin from "@typescript-eslint/eslint-plugin";
import eslintReact from "@eslint-react/eslint-plugin";
import reactHooksPlugin from "eslint-plugin-react-hooks";
import prettierConfig from "eslint-config-prettier";

export default [
  {
    ignores: ["dist/**", "node_modules/**"],
  },
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      parser: tsParser,
      parserOptions: {
        ecmaVersion: "latest",
        sourceType: "module",
        ecmaFeatures: { jsx: true },
      },
    },
    plugins: {
      "@typescript-eslint": tsPlugin,
      ...eslintReact.configs["recommended-typescript"].plugins,
      "react-hooks": reactHooksPlugin,
    },
    settings: eslintReact.configs["recommended-typescript"].settings,
    rules: {
      ...tsPlugin.configs.recommended.rules,
      ...eslintReact.configs["recommended-typescript"].rules,
      ...reactHooksPlugin.configs.recommended.rules,
      ...eslintReact.configs["disable-conflict-eslint-plugin-react-hooks"].rules,
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_", caughtErrorsIgnorePattern: "^_" },
      ],
      "@typescript-eslint/no-explicit-any": "off",
    },
  },
  prettierConfig,
];

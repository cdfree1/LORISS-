import eslint from "@eslint/js";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: ["dist/**", "node_modules/**"],
  },
  eslint.configs.recommended,
  ...tseslint.configs.recommended,
  {
    languageOptions: {
      globals: {
        document: "readonly",
        fetch: "readonly",
        HTMLElement: "readonly",
        HTMLButtonElement: "readonly",
        HTMLParagraphElement: "readonly",
        URL: "readonly",
      },
    },
  },
);

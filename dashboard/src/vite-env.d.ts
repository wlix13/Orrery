/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_MOCK?: string;
  /** "1" trims the mock fleet fixtures down to one fleet. */
  readonly VITE_MOCK_SINGLE_FLEET?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

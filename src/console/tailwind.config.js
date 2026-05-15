/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        brand: {
          surface: {
            base: "var(--surface-base)",
            subtle: "var(--surface-subtle)",
            raised: "var(--surface-raised)",
            inverse: "var(--surface-inverse)",
          },
          text: {
            primary: "var(--text-primary)",
            secondary: "var(--text-secondary)",
            muted: "var(--text-muted)",
            inverse: "var(--text-inverse)",
          },
          border: {
            DEFAULT: "var(--border-default)",
            strong: "var(--border-strong)",
            focus: "var(--border-focus)",
          },
          action: {
            primary: {
              background: "var(--action-primary-background)",
              text: "var(--action-primary-text)",
            },
            secondary: {
              background: "var(--action-secondary-background)",
            },
            danger: {
              background: "var(--action-danger-background)",
            },
          },
          status: {
            success: "var(--status-success)",
            warning: "var(--status-warning)",
            error: "var(--status-error)",
            info: "var(--status-info)",
          },
        },
      },
      fontFamily: {
        body: "var(--font-body)",
        heading: "var(--font-heading)",
        mono: "var(--font-mono)",
      },
      boxShadow: {
        ui: "var(--ui-shadow-card)",
        "ui-control": "var(--ui-shadow-control)",
        "ui-active": "var(--ui-shadow-active)",
      },
      borderRadius: {
        ui: "var(--ui-radius-control)",
        "ui-surface": "var(--ui-radius-surface)",
      },
    },
  },
  plugins: [],
};

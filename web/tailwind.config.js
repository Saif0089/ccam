/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        ground: "#0F1216", raised: "#171B21", "raised-2": "#1E242D", sunken: "#0B0E12", line: "#262C34",
        ink: "#E7EBF0", muted: "#9AA4B2", faint: "#626D7C",
        primary: "#6E8BFF", ok: "#46C08A", warn: "#E0A83E", crit: "#E05C53",
        fable: "#C77DFF", opus: "#6E8BFF", sonnet: "#46C08A", haiku: "#E0A83E",
      },
      fontFamily: {
        sans: ["-apple-system", "BlinkMacSystemFont", "Segoe UI", "Roboto", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
      },
    },
  },
  plugins: [],
};

export interface BrandTheme {
  name: string
  primary: string
  secondary: string
  tertiary: string
  primaryRgb: string
  secondaryRgb: string
  lightAccent: string
}

export const THEMES: Record<string, BrandTheme> = {
  azure: {
    name: "Azure",
    primary: "#0347A5",
    secondary: "#7C3AED",
    tertiary: "#EC4899",
    primaryRgb: "3, 71, 165",
    secondaryRgb: "124, 58, 237",
    lightAccent: "#B3D1FF",
  },
  emerald: {
    name: "Emerald",
    primary: "#059669",
    secondary: "#0D9488",
    tertiary: "#06B6D4",
    primaryRgb: "5, 150, 105",
    secondaryRgb: "13, 148, 136",
    lightAccent: "#A7F3D0",
  },
  ruby: {
    name: "Ruby",
    primary: "#BE123C",
    secondary: "#E11D48",
    tertiary: "#FB7185",
    primaryRgb: "190, 18, 60",
    secondaryRgb: "225, 29, 72",
    lightAccent: "#FDA4AF",
  },
  amber: {
    name: "Amber",
    primary: "#B45309",
    secondary: "#D97706",
    tertiary: "#FBBF24",
    primaryRgb: "180, 83, 9",
    secondaryRgb: "217, 119, 6",
    lightAccent: "#FDE68A",
  },
  orange: {
    name: "Orange",
    primary: "#C2410C",
    secondary: "#EA580C",
    tertiary: "#F97316",
    primaryRgb: "194, 65, 12",
    secondaryRgb: "234, 88, 12",
    lightAccent: "#FED7AA",
  },
}

export function applyTheme(theme: BrandTheme) {
  const root = document.documentElement.style
  root.setProperty("--brand-primary", theme.primary)
  root.setProperty("--brand-secondary", theme.secondary)
  root.setProperty("--brand-tertiary", theme.tertiary)
  root.setProperty("--brand-primary-rgb", theme.primaryRgb)
  root.setProperty("--brand-secondary-rgb", theme.secondaryRgb)
  root.setProperty("--brand-light", theme.lightAccent)
}

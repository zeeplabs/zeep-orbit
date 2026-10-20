import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import ptBR from '../locales/pt-BR.json'
import en from '../locales/en.json'

const saved = localStorage.getItem('orbit-lang')

// Keep <html lang> in sync so screen readers pick the right voice/pronunciation.
// Registered before init so it also fires for the initial language.
i18n.on('languageChanged', (lng) => {
  document.documentElement.lang = lng
})

i18n.use(initReactI18next).init({
  resources: {
    'pt-BR': { translation: ptBR },
    en: { translation: en },
  },
  lng: saved || 'en',
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
})

export function setLanguage(lang: string) {
  i18n.changeLanguage(lang)
  localStorage.setItem('orbit-lang', lang)
}

export default i18n

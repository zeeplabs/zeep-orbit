import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import ptBR from '../locales/pt-BR.json'
import en from '../locales/en.json'

const saved = localStorage.getItem('orbit-lang')

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

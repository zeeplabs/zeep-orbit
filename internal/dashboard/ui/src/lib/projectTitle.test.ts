import { describe, test, expect } from 'vitest'
import { projectTitleFromPrompt, PROJECT_TITLE_MAX } from './projectTitle'

describe('projectTitleFromPrompt', () => {
  test('a short prompt is the title as written', () => {
    expect(projectTitleFromPrompt('App para minha clínica')).toBe('App para minha clínica')
  })

  test('blank prompt gives an empty title', () => {
    expect(projectTitleFromPrompt('')).toBe('')
    expect(projectTitleFromPrompt('  \n\t ')).toBe('')
  })

  test('collapses and trims whitespace, newlines included', () => {
    expect(projectTitleFromPrompt('  um   app\n\npara  a clínica  ')).toBe('um app para a clínica')
  })

  test('exactly 60 characters is kept whole', () => {
    const sixty = 'a'.repeat(30) + ' ' + 'b'.repeat(29)
    expect(sixty).toHaveLength(60)
    expect(projectTitleFromPrompt(sixty)).toBe(sixty)
  })

  test('cuts at the last word boundary before 60 characters', () => {
    const prompt = 'Quero um aplicativo para gerenciar o agendamento de consultas da minha clínica odontológica'
    const title = projectTitleFromPrompt(prompt)
    expect(title).toBe('Quero um aplicativo para gerenciar o agendamento de')
    expect(Array.from(title).length).toBeLessThanOrEqual(PROJECT_TITLE_MAX)
    expect(prompt.startsWith(title)).toBe(true)
  })

  test('a cut that lands right before a space keeps the word and drops the space', () => {
    const prompt = 'a'.repeat(59) + ' ' + 'b'.repeat(10)
    expect(projectTitleFromPrompt(prompt)).toBe('a'.repeat(59))
  })

  test('a single word longer than the limit is cut hard', () => {
    expect(projectTitleFromPrompt('x'.repeat(200))).toBe('x'.repeat(60))
  })

  test('counts characters, not UTF-16 units: no split emoji or accent', () => {
    const emojis = '😀'.repeat(100)
    const title = projectTitleFromPrompt(emojis)
    expect(Array.from(title)).toHaveLength(60)
    expect(title).toBe('😀'.repeat(60))
    expect(projectTitleFromPrompt('ç'.repeat(70))).toBe('ç'.repeat(60))
  })

  test('non-ASCII words are cut on spaces too', () => {
    const prompt = 'ação '.repeat(30)
    const title = projectTitleFromPrompt(prompt)
    expect(Array.from(title).length).toBeLessThanOrEqual(60)
    expect(title.endsWith('ação')).toBe(true)
  })

  test('respects a custom limit', () => {
    expect(projectTitleFromPrompt('um dois três quatro', 10)).toBe('um dois')
  })
})

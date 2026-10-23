import { useEffect, useState } from 'react'

// Formulários sujos registram-se aqui para que um único <UnsavedChangesBlocker />
// (no shell, dentro do data router) intercepte a navegação interna sem que cada
// formulário precise renderizar o próprio diálogo.
const dirtyGuards = new Set<symbol>()
const listeners = new Set<() => void>()

function emit() {
  listeners.forEach((l) => l())
}

let leavingConfirmed = false

export function hasUnsavedChanges(): boolean {
  return dirtyGuards.size > 0
}

/**
 * Para uma saída que o usuário já confirmou dentro do app (ex.: logout com aviso de
 * alterações não salvas): a próxima navegação completa não mostra o prompt nativo.
 */
export function confirmLeavingWithUnsavedChanges(): void {
  leavingConfirmed = true
}

/** Desfaz `confirmLeavingWithUnsavedChanges()` quando a saída não aconteceu (ex.: erro no logout). */
export function resetLeavingConfirmation(): void {
  leavingConfirmed = false
}

export function subscribeUnsavedChanges(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/**
 * Protege edição não salva enquanto `dirty` for true:
 * - fechar/recarregar a aba ou digitar outra URL: confirmação do navegador (`beforeunload`;
 *   o texto é do navegador e não pode ser customizado);
 * - navegação interna (links, voltar/avançar, troca de aba `?tab=`): bloqueada por
 *   <UnsavedChangesBlocker />, que usa `useBlocker` e por isso exige data router
 *   (`createBrowserRouter` em main.tsx).
 */
export function useUnsavedChangesGuard(dirty: boolean) {
  const [id] = useState(() => Symbol('unsaved-guard'))
  useEffect(() => {
    if (!dirty) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (leavingConfirmed) return
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    dirtyGuards.add(id)
    emit()
    return () => {
      window.removeEventListener('beforeunload', onBeforeUnload)
      dirtyGuards.delete(id)
      emit()
    }
  }, [dirty, id])
}

/**
 * Para formulários que se reidratam a partir de um valor do servidor: compara os
 * campos atuais com `initial` (o que o servidor devolveu) e, depois de um save
 * bem-sucedido (`markSaved()`), com o que foi salvo. Ativa `useUnsavedChangesGuard`
 * enquanto diferirem. Os dois argumentos devem ter as mesmas chaves, na mesma ordem.
 *
 * O snapshot salvo só vale até o servidor devolver um `initial` novo (refetch após o
 * save): daí em diante a comparação volta a ser contra o servidor. Sem isso, um valor
 * que o servidor normaliza (default preenchido, lista reformatada) deixaria o form
 * "sujo" para sempre depois do save. `markSaved(values)` aceita os valores efetivamente
 * salvos quando o form os altera ao salvar (ex.: limpar um campo de segredo).
 */
export function useFormDirtyGuard(initial: unknown, current: unknown) {
  const [saved, setSaved] = useState<{ initial: string; current: string } | null>(null)
  const initialSnapshot = JSON.stringify(initial)
  const currentSnapshot = JSON.stringify(current)
  const baseline = saved && saved.initial === initialSnapshot ? saved.current : initialSnapshot
  useUnsavedChangesGuard(currentSnapshot !== baseline)
  return {
    markSaved: (values?: unknown) =>
      setSaved({
        initial: initialSnapshot,
        current: values === undefined ? currentSnapshot : JSON.stringify(values),
      }),
  }
}

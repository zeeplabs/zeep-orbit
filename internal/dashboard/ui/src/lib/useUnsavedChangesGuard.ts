import { useEffect, useState } from 'react'

// Formulários sujos registram-se aqui para que um único <UnsavedChangesBlocker />
// (no shell, dentro do data router) intercepte a navegação interna sem que cada
// formulário precise renderizar o próprio diálogo.
const dirtyGuards = new Set<symbol>()
const listeners = new Set<() => void>()

function emit() {
  listeners.forEach((l) => l())
}

export function hasUnsavedChanges(): boolean {
  return dirtyGuards.size > 0
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
 */
export function useFormDirtyGuard(initial: unknown, current: unknown) {
  const [savedSnapshot, setSavedSnapshot] = useState<string | null>(null)
  const currentSnapshot = JSON.stringify(current)
  useUnsavedChangesGuard(currentSnapshot !== (savedSnapshot ?? JSON.stringify(initial)))
  return { markSaved: () => setSavedSnapshot(currentSnapshot) }
}

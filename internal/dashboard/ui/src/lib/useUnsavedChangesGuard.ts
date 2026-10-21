import { useEffect, useState } from 'react'

/**
 * Pede confirmação do navegador ao fechar/recarregar a aba enquanto há edição não salva.
 *
 * Só cobre saída da página (fechar, recarregar, digitar outra URL). Navegação
 * interna do app NÃO é interceptada: o app usa `BrowserRouter`, e `useBlocker`
 * só existe em data routers (`createBrowserRouter`). O texto do aviso é do
 * navegador e não pode ser customizado.
 */
export function useUnsavedChangesGuard(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [dirty])
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

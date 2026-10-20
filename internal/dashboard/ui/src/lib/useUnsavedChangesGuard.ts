import { useEffect } from 'react'

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

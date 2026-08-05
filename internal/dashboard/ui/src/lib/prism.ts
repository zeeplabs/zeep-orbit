import Prism from 'prismjs'
import 'prismjs/components/prism-clike'
import 'prismjs/components/prism-javascript'
import 'prismjs/components/prism-typescript'
import 'prismjs/components/prism-go'
import 'prismjs/components/prism-python'
import 'prismjs/components/prism-rust'
import 'prismjs/components/prism-java'
import 'prismjs/components/prism-markup'
import 'prismjs/components/prism-markup-templating'
import 'prismjs/components/prism-php'

export type SdkLang = 'typescript' | 'go' | 'python' | 'rust' | 'java' | 'php'

export function highlight(code: string, lang: SdkLang): string {
  const grammar = Prism.languages[lang]
  if (!grammar) return code
  return Prism.highlight(code, grammar, lang)
}

import type { HighlighterCore } from 'shiki/core'

/** Languages we ship. Add here (and only here) to support more. */
export const languages = ['json', 'yaml', 'bash', 'typescript', 'python', 'http', 'markdown'] as const
export type CodeLanguage = (typeof languages)[number] | 'text'

let highlighter: Promise<HighlighterCore> | null = null

/** Lazily create a single Shiki highlighter using a CSS-variables theme (colors come from tokens.css). */
export function getHighlighter(): Promise<HighlighterCore> {
  highlighter ??= (async () => {
    const [{ createHighlighterCore, createCssVariablesTheme }, { createJavaScriptRegexEngine }] = await Promise.all([
      import('shiki/core'),
      import('shiki/engine/javascript'),
    ])
    return createHighlighterCore({
      themes: [createCssVariablesTheme({ name: 'airval', variablePrefix: '--shiki-', fontStyle: true })],
      langs: [
        import('shiki/langs/json.mjs'),
        import('shiki/langs/yaml.mjs'),
        import('shiki/langs/bash.mjs'),
        import('shiki/langs/typescript.mjs'),
        import('shiki/langs/python.mjs'),
        import('shiki/langs/http.mjs'),
        import('shiki/langs/markdown.mjs'),
      ],
      engine: createJavaScriptRegexEngine(),
    })
  })()
  return highlighter
}

export async function highlight(code: string, lang: CodeLanguage): Promise<string> {
  const h = await getHighlighter()
  return h.codeToHtml(code, { lang: lang === 'text' ? 'text' : lang, theme: 'airval' })
}

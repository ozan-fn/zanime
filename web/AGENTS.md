# AGENTS.md

## Commands

- `pnpm run dev` - Start the dev server
- `pnpm run build` - Build the app for production
- `pnpm run preview` - Preview the production build locally
- `pnpm run lint` - Lint the code
- `npx tsc --noEmit` - Typecheck

## Stack

React 19 + `react-router` 8 (declarative: `BrowserRouter`, `<Routes>`,
`<Route element>`), bundled by rsbuild with Tailwind 4. React Compiler is on via
`pluginReact({ reactCompiler: true })` in `rsbuild.config.ts` — do not add
manual `useMemo`/`useCallback` for re-render reasons; the compiler handles it.

## Docs

- React 19: https://react.dev/llms.txt
- React Compiler: https://react.dev/learn/react-compiler
- React Router: https://reactrouter.com/start/declarative/routing
- Rsbuild: https://rsbuild.rs/llms.txt
- Rsbuild React plugin (incl. `reactCompiler`): https://rsbuild.rs/plugins/list/plugin-react
- Rspack: https://rspack.rs/llms.txt
- Rslint: https://rslint.rs/llms.txt

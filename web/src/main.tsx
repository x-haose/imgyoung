import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import {
  MutationCache,
  QueryClient,
  QueryClientProvider,
} from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import '@fontsource/ibm-plex-mono/400.css'
import '@fontsource/ibm-plex-sans/400.css'
import '@fontsource/ibm-plex-sans/500.css'
import '@fontsource/ibm-plex-sans/600.css'
import { ApiError, onUnauthorized } from '@/lib/api'
import { hasStatus, toastError } from '@/lib/errors'
import { sessionQueryKey } from '@/lib/session'
import { LocaleProvider } from '@/context/locale-provider'
import { ThemeProvider } from '@/context/theme-provider'
import { routeTree } from './routeTree.gen'
import './styles/index.css'

/**
 * 会话失效时去登录页,回跳地址是当前页。一次失效会让在途的几个请求先后报 401,
 * 已在登录页即不再导航,否则登录页自己会被当成回跳地址。
 */
function toLogin() {
  const { pathname, href } = router.history.location
  if (pathname === '/login') return
  router.navigate({ to: '/login', search: { redirect: href }, replace: true })
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 4xx 是确定性结果,重试无益;网络层失败与 5xx 再试一次。
      retry: (failureCount, error) =>
        failureCount < 1 &&
        !(
          error instanceof ApiError &&
          error.status >= 400 &&
          error.status < 500
        ),
      staleTime: 10_000,
    },
  },
  mutationCache: new MutationCache({
    onError: (error, _vars, _ctx, mutation) => {
      // 自带 onError 的 mutation 就地展示错误;401 已由会话失效处理跳转。
      if (mutation.options.onError || hasStatus(error, 401)) return
      toastError(error)
    },
  }),
})

onUnauthorized(() => {
  queryClient.removeQueries({ queryKey: sessionQueryKey })
  toLogin()
})

const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: 'intent',
  defaultPreloadStaleTime: 0,
  scrollRestoration: true,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

const rootElement = document.getElementById('root')
if (!rootElement) throw new Error('缺少 #root 挂载点')
ReactDOM.createRoot(rootElement).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <LocaleProvider>
        <ThemeProvider>
          <RouterProvider router={router} />
        </ThemeProvider>
      </LocaleProvider>
    </QueryClientProvider>
  </StrictMode>
)

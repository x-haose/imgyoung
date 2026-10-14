import globals from 'globals'
import js from '@eslint/js'
import pluginQuery from '@tanstack/eslint-plugin-query'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import { defineConfig } from 'eslint/config'
import tseslint from 'typescript-eslint'

export default defineConfig(
  // src/components/ui 是 shadcn 的组件源码;routeTree.gen.ts 由路由插件生成,api.gen.ts 由接口文档生成。
  {
    ignores: [
      'dist',
      'src/components/ui',
      'src/routeTree.gen.ts',
      'src/lib/api.gen.ts',
    ],
  },
  {
    extends: [
      js.configs.recommended,
      ...tseslint.configs.recommended,
      ...pluginQuery.configs['flat/recommended'],
    ],
    files: ['**/*.{ts,tsx}'],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser,
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-refresh/only-export-components': [
        'warn',
        {
          allowConstantExport: true,
          allowExportNames: [
            'useTheme',
            'useLayout',
            'useSearch',
            'useLocale',
            'useMessages',
          ],
        },
      ],
      // 未启用 React Compiler。
      'react-hooks/incompatible-library': 'off',
      'no-console': 'error',
      'no-unused-vars': 'off',
      '@typescript-eslint/no-unused-vars': [
        'error',
        {
          args: 'all',
          argsIgnorePattern: '^_',
          caughtErrors: 'all',
          caughtErrorsIgnorePattern: '^_',
          destructuredArrayIgnorePattern: '^_',
          varsIgnorePattern: '^_',
          ignoreRestSiblings: true,
        },
      ],
      '@typescript-eslint/consistent-type-imports': [
        'error',
        {
          prefer: 'type-imports',
          fixStyle: 'inline-type-imports',
          disallowTypeAnnotations: false,
        },
      ],
      'no-duplicate-imports': 'error',
    },
  },
  {
    // 声明合并的类型参数须与原声明一致。
    files: ['src/tanstack-table.d.ts'],
    rules: { '@typescript-eslint/no-unused-vars': 'off' },
  },
  {
    // 路由定义打进首屏入口块,zod 只该随表单页按需加载;搜索参数用 @/lib/search。
    files: ['src/routes/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': [
        'error',
        {
          paths: [
            {
              name: 'zod',
              message: '路由文件不引入 zod,搜索参数用 @/lib/search',
            },
          ],
        },
      ],
    },
  }
)

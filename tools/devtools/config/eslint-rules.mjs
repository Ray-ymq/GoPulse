// Pure rule data shared by both frontend ESLint flat configurations.
export default {
  'no-debugger': 'error',
  'no-async-promise-executor': 'error',
  'no-constant-condition': ['error', { checkLoops: false }],
  'vue/no-mutating-props': 'error',
  'vue/no-side-effects-in-computed-properties': 'error',
}

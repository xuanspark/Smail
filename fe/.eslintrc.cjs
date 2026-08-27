/* eslint-env node */
module.exports = {
  root: true,
  'extends': [
    'plugin:vue/vue3-essential',
    'eslint:recommended'
  ],
  parserOptions: {
    ecmaVersion: 'latest'
  },
  overrides: [
    {
      files: ['src/views/**/*.vue', 'src/views/components/**/*.vue'],
      rules: {
        'vue/multi-word-component-names': 'off'
      }
    }
  ]
}

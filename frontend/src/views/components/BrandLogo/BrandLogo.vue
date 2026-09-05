<template>
  <span v-if="surface" :class="['brand-logo-frame', `brand-logo-frame--${variant}`]">
    <img :src="src" :alt="alt" :class="['brand-logo', `brand-logo--${variant}`]" />
  </span>
  <img v-else :src="src" :alt="alt" :class="['brand-logo', `brand-logo--${variant}`]" />
</template>

<script setup>
import { brandLogoProps } from './BrandLogo.js'

const {
  alt,
  src,
  variant,
  surface
} = defineProps(brandLogoProps)
</script>

<style scoped src="./BrandLogo.css"></style>

<script setup lang="ts">
import ProductIcon from '../../../shared/ProductIcon.vue'

defineOptions({ inheritAttrs: false })

withDefaults(defineProps<{ name: string; size?: number }>(), { size: 20 })
</script>

<template>
  <ProductIcon v-bind="$attrs" :name="name" :size="size" class="admin-icon" />
</template>

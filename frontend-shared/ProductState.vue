<script setup lang="ts">
import ProductIcon from './ProductIcon.vue'

export type ProductStateName = 'loading' | 'empty' | 'partial' | 'stale' | 'denied' | 'unavailable'

const props = withDefaults(defineProps<{
  state: ProductStateName
  message?: string
  title?: string
  actionLabel?: string
  actionDisabled?: boolean
}>(), { actionDisabled: false })

const emit = defineEmits<{ retry: [] }>()

const copy: Record<ProductStateName, { title: string; message: string; icon: string }> = {
  loading: { title: '正在加载', message: '正在读取最新数据…', icon: 'refresh' },
  empty: { title: '暂无数据', message: '当前条件下还没有可展示的记录。', icon: 'box' },
  partial: { title: '部分数据可用', message: '部分依赖暂不可用，以下内容可能不完整。', icon: 'info' },
  stale: { title: '数据已过期', message: '当前显示上次成功结果，请刷新获取最新数据。', icon: 'clock' },
  denied: { title: '无权访问', message: '当前账号无权访问此内容，请联系管理员确认权限。', icon: 'warning' },
  unavailable: { title: '服务暂不可用', message: '读取依赖暂时不可用，请稍后重试。', icon: 'warning' },
}

const current = () => copy[props.state]
</script>

<template>
  <section
    class="gp-state state-card"
    :class="`gp-state--${state}`"
    :data-state="state"
    :role="state === 'denied' || state === 'unavailable' ? 'alert' : 'status'"
    aria-live="polite"
  >
    <h3 class="gp-state__heading">
      <ProductIcon :name="current().icon" :size="18" />
      {{ title || current().title }}
    </h3>
    <p class="gp-state__message"><slot>{{ message || current().message }}</slot></p>
    <div v-if="$slots.actions || actionLabel" class="gp-state__actions">
      <slot name="actions">
        <button v-if="actionLabel" class="button button--secondary" type="button" :disabled="actionDisabled" @click="emit('retry')">
          {{ actionLabel }}
        </button>
      </slot>
    </div>
  </section>
</template>

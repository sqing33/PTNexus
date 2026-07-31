<template>
  <div v-if="summary.hasSummary" class="mediainfo-summary-card">
    <div class="mediainfo-summary-card__title">
      <span>{{ summary.fileName }}</span>
    </div>

    <div class="mediainfo-summary-card__grid">
      <section class="mediainfo-summary-card__section">
        <div class="mediainfo-summary-card__section-title">General</div>
        <div class="mediainfo-summary-card__section-body">
          <div
            v-for="field in summary.general"
            :key="field.label"
            class="mediainfo-summary-card__field"
          >
            <span class="mediainfo-summary-card__field-label">{{ field.label }}:</span>
            <span class="mediainfo-summary-card__field-value">{{ field.value }}</span>
          </div>
        </div>
      </section>

      <section class="mediainfo-summary-card__section">
        <div class="mediainfo-summary-card__section-title">Video</div>
        <div class="mediainfo-summary-card__section-body">
          <div
            v-for="field in summary.video"
            :key="field.label"
            class="mediainfo-summary-card__field"
          >
            <span class="mediainfo-summary-card__field-label">{{ field.label }}:</span>
            <span class="mediainfo-summary-card__field-value">{{ field.value }}</span>
          </div>
        </div>
      </section>

      <section class="mediainfo-summary-card__section">
        <div class="mediainfo-summary-card__section-title">
          Audio<span v-if="summary.audio.length">({{ summary.audio.length }})</span>
        </div>
        <div class="mediainfo-summary-card__section-body">
          <div
            v-for="(line, index) in summary.audio"
            :key="`${line}-${index}`"
            class="mediainfo-summary-card__line"
          >
            {{ line }}
          </div>
        </div>
      </section>

      <section class="mediainfo-summary-card__section">
        <div class="mediainfo-summary-card__section-title">
          Subtitles<span v-if="summary.subtitles.length">({{ summary.subtitles.length }})</span>
        </div>
        <div class="mediainfo-summary-card__section-body">
          <div
            v-for="(line, index) in summary.subtitles"
            :key="`${line}-${index}`"
            class="mediainfo-summary-card__line"
          >
            {{ line }}
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { buildMediaInfoSummary } from './panel/mediaInfoSummary'

const props = defineProps<{
  text?: string
}>()

const summary = computed(() => buildMediaInfoSummary(props.text || ''))
</script>

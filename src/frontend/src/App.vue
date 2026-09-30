<template>
  <router-view />
  <DialogHost />
  <AgentPanel />
</template>

<script setup>
import DialogHost from './components/dialogs/DialogHost.vue'
import AgentPanel from './agent/AgentPanel.vue'
</script>

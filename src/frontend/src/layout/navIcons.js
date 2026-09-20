// 侧边栏导航图标（内联 SVG 单一约定，16px 描边风格）。
// 从 AppShell.vue 抽出独立成模块：AppShell 自身需控制行数（frontend_org_guard 棘轮），
// 且图标集合与布局逻辑无耦合。新增导航项在此加条目即可。
export const navIcons = {
  timeline:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><rect x="2" y="2" width="12" height="3" rx="1" stroke="currentColor" stroke-width="1.4"/><rect x="2" y="7" width="12" height="3" rx="1" stroke="currentColor" stroke-width="1.4"/><rect x="2" y="12" width="12" height="3" rx="1" stroke="currentColor" stroke-width="1.4"/></svg>',
  album:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><rect x="2" y="2" width="12" height="12" rx="2" stroke="currentColor" stroke-width="1.4"/><path d="M2 10l3.5-3.5 3 3L11 7l3 3" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/></svg>',
  person:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><circle cx="8" cy="5.5" r="2.8" stroke="currentColor" stroke-width="1.4"/><path d="M2.5 14c.8-2.6 2.9-4 5.5-4s4.7 1.4 5.5 4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>',
  place:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><path d="M8 14.5S3 10 3 6.5A5 5 0 0 1 13 6.5C13 10 8 14.5 8 14.5z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><circle cx="8" cy="6.5" r="1.8" stroke="currentColor" stroke-width="1.4"/></svg>',
  tag: '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><path d="M2 2h5l7 7-5 5-7-7V2z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><circle cx="5.5" cy="5.5" r="1" fill="currentColor"/></svg>',
  folder:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><path d="M2 4a1 1 0 0 1 1-1h3.6l1.6 2H13a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V4z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/></svg>',
  settings:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><circle cx="8" cy="8" r="2.2" stroke="currentColor" stroke-width="1.4"/><path d="M8 1.8v2M8 12.2v2M1.8 8h2M12.2 8h2M3.6 3.6l1.4 1.4M11 11l1.4 1.4M12.4 3.6L11 5M5 11l-1.4 1.4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>',
  map: '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><path d="M2 4l4-1.5 4 1.5 4-1.5v10L10 14l-4-1.5L2 14V4z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M6 2.5v11M10 4v11" stroke="currentColor" stroke-width="1.4"/></svg>',
  toolbox:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><rect x="2" y="5.5" width="12" height="8.5" rx="1.5" stroke="currentColor" stroke-width="1.4"/><path d="M6 5.5V4a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v1.5M2 9h12" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>',
  spaces:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><ellipse cx="8" cy="4" rx="6" ry="2.2" stroke="currentColor" stroke-width="1.4"/><path d="M2 4v4c0 1.2 2.7 2.2 6 2.2s6-1 6-2.2V4M2 8v4c0 1.2 2.7 2.2 6 2.2s6-1 6-2.2V8" stroke="currentColor" stroke-width="1.4"/></svg>',
  admin:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><path d="M8 1.8l5.5 2v4c0 3.4-2.3 5.7-5.5 6.8-3.2-1.1-5.5-3.4-5.5-6.8v-4l5.5-2z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M5.8 8l1.5 1.5 3-3" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg>'
}

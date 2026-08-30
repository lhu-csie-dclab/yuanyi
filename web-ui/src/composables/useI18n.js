import { reactive, computed } from 'vue'

const STORAGE_KEY = 'preferred_lang'

const state = reactive({
  lang: localStorage.getItem(STORAGE_KEY) || 'en',
})

const messages = {
  en: {
    // ── Nav ──────────────────────────────────────────────────────────
    nav_monitor:      'Monitor',
    nav_topology:     'Topology',
    nav_top:          'Top Ranking',
    nav_chat:         'Chat',
    nav_logs:         'Logs',
    nav_manage:       'Manage',
    nav_settings:     'Settings',
    nav_dev:          'Developer',
    nav_api:          'OpenAI API',
    nav_cluster:      'Cluster Hub',
    nav_hub_topology: 'Node Topology',
    nav_hub_history:  'History',
    nav_hub_board:    'Leaderboard',
    page_top:         'Top Ranking',

    // ── Status ───────────────────────────────────────────────────────
    status_online:    'Online',
    status_offline:   'Offline',
    status_connecting:'Connecting…',
    status_healthy:   'Healthy',
    status_retry:     'Retry',

    // ── Topology view ────────────────────────────────────────────────
    page_topology:    'Topology & Ranking',
    badge_nodes:      (n) => `${n} Nodes`,
    stat_nodes:       'Nodes',
    stat_requests:    'Requests',
    stat_throughput:  'Throughput t/s',
    stat_ttft:        'TTFT (s)',
    stat_power:       'Total Power',
    stat_cpu:         'CPU',
    stat_memory:      'Memory',
    section_local:    'Local Node Contribution',
    stat_total_tokens:'Total Tokens',
    stat_input:       'Input',
    stat_output:      'Output',
    stat_uptime:      'Uptime',
    section_peers:    'P2P Peers',
    peer_count:       (n) => `${n} Nodes`,
    top_nodes:        'Top Nodes',
    by_throughput:    'by GPU tier (VRAM)',
    col_rank:         '#',
    col_node_id:      'Node ID',
    col_ip:           'IP',
    col_gpu:          'GPU',
    col_health:       'Health',
    col_telemetry:    'Telemetry',
    col_engine:       'Engine',
    col_status:       'Status',
    no_gpu:           'No GPU',
    local_badge:      'Local',
    relay_badge:      'Relay-only',
    loading_nodes:    'Searching nodes…',
    view_grid:        'Grid',
    view_list:        'List',
    gpu_nodes_count:  (n) => `${n} GPU`,
    relay_nodes_count:(n) => `${n} relay`,
    busy_now_count:   (n) => `${n} busy`,
    copy_id:          'Copy ID',
    copied:           'Copied',

    // ── Hub Topology ─────────────────────────────────────────────────
    page_hub_topology:   'Cluster Topology',
    section_cluster:     'Cluster Contribution',
    section_node_list:   'Node List',
    stat_cluster_tokens: 'Total Tokens',
    penalty:             (n) => `Penalty ${n}`,
    hub_admin:           'Hub Admin',
    btn_force_rank:      'Force Re-rank',
    btn_clear_offline:   'Clear Offline',
    retry_label:         (n) => `Retry ${n}/3`,
    loading_cluster:     'Searching nodes…',

    // ── Leaderboard ──────────────────────────────────────────────────
    page_leaderboard:  'Leaderboard',
    section_board:     'Node Contribution Ranking',
    col_rank_label:    'Rank',
    col_peer:          'Node',
    col_tasks:         'Requests',
    col_in_tokens:     'Input',
    col_out_tokens:    'Output',
    col_total_tokens:  'Total Tokens',
    col_score:         'Score',
    col_score_hint:    'Hardware tier only (VRAM x 300 x GPU count) -- not affected by traffic or uptime',
    no_records:        'No records',

    // ── History ──────────────────────────────────────────────────────
    page_history:      'Audit Log',
    section_events:    'Global Event Log',
    col_time:          'Time',
    col_event:         'Event',
    col_retries:       'Retries',
    col_penalties:     'Penalties',
    col_detail:        'Detail',
    no_events:         'No events',
    records_count:     (n) => `${n} records`,
    evt_fail:          'Failed',
    evt_recover:       'Recovered',
    evt_offline:       'Offline',
    evt_join:          'Joined',

    // ── Chat ─────────────────────────────────────────────────────────
    page_chat:         'Chat',
    chat_new:          'New',
    chat_sessions:     'Sessions',
    chat_settings:     'Model Settings',
    chat_clear:        'Clear',
    chat_streaming:    'Streaming…',
    chat_placeholder:  'Type a message… (Enter to send, Shift+Enter for newline)',
    chat_empty_title:  'Start a new conversation',
    chat_storage_hint: 'Session auto-saved to localStorage',
    chat_attach_image: 'Attach image (requires a vision-capable model on the serving node)',
    chat_remove_image: 'Remove image',
    cfg_endpoint:      'API Endpoint',
    cfg_model:         'Model',
    cfg_api_key:       'API Key (optional)',
    cfg_temperature:   'Temperature',
    cfg_max_tokens:    'Max Tokens',
    cfg_system:        'System Prompt (optional)',
    cfg_from_config:   (proxy, vllm, model) => `config.json → proxy :${proxy}  ·  vLLM :${vllm}  ·  model: ${model || '(not set)'}`,
    cfg_reading:       'Reading config.json…',
    cfg_reset:         '↺ Reset from config.json',
    chat_user_me:      'Me',

    // ── Logs ─────────────────────────────────────────────────────────
    page_logs:         'Live Logs',

    // ── Settings ─────────────────────────────────────────────────────
    page_settings:     'Settings & Backup',
  },

  zh: {
    // ── Nav ──────────────────────────────────────────────────────────
    nav_monitor:      '監控',
    nav_topology:     '節點拓撲',
    nav_top:          'TOP 排名',
    nav_chat:         '聊天室',
    nav_logs:         '即時日誌',
    nav_manage:       '管理',
    nav_settings:     '設定 & 備份',
    nav_dev:          '開發',
    nav_api:          'OpenAI API',
    nav_cluster:      '叢集 Hub',
    nav_hub_topology: '節點拓撲',
    nav_hub_history:  '歷史紀錄',
    nav_hub_board:    '排行榜',
    page_top:         'TOP 節點排名',

    // ── Status ───────────────────────────────────────────────────────
    status_online:    '在線',
    status_offline:   '離線',
    status_connecting:'連接中…',
    status_healthy:   '正常',
    status_retry:     '重試',

    // ── Topology view ────────────────────────────────────────────────
    page_topology:    '拓撲 & 排名',
    badge_nodes:      (n) => `${n} 節點`,
    stat_nodes:       '節點數',
    stat_requests:    '活躍請求',
    stat_throughput:  '吞吐量 t/s',
    stat_ttft:        'TTFT (s)',
    stat_power:       '總功耗',
    stat_cpu:         'CPU',
    stat_memory:      '記憶體',
    section_local:    '本機節點貢獻',
    stat_total_tokens:'總 Tokens',
    stat_input:       '輸入',
    stat_output:      '輸出',
    stat_uptime:      '在線時間',
    section_peers:    'P2P 節點',
    peer_count:       (n) => `${n} 個節點`,
    top_nodes:        '前幾名節點 (TOP 排名)',
    by_throughput:    '依顯卡等級(VRAM)排序',
    col_rank:         '#',
    col_node_id:      '節點 ID',
    col_ip:           'IP',
    col_gpu:          'GPU',
    col_health:       '健康',
    col_telemetry:    '遙測',
    col_engine:       '引擎',
    col_status:       '狀態',
    no_gpu:           '無 GPU',
    local_badge:      '本機',
    relay_badge:      '純中繼',
    loading_nodes:    '搜尋節點中…',
    view_grid:        '卡片',
    view_list:        '列表',
    gpu_nodes_count:  (n) => `${n} 個 GPU`,
    relay_nodes_count:(n) => `${n} 個中繼`,
    busy_now_count:   (n) => `${n} 忙碌中`,
    copy_id:          '複製 ID',
    copied:           '已複製',

    // ── Hub Topology ─────────────────────────────────────────────────
    page_hub_topology:   '叢集拓撲',
    section_cluster:     '叢集總貢獻',
    section_node_list:   '節點清單',
    stat_cluster_tokens: '總 Tokens',
    penalty:             (n) => `罰 ${n}`,
    hub_admin:           'Hub 管理',
    btn_force_rank:      '強制重排名',
    btn_clear_offline:   '清除離線節點',
    retry_label:         (n) => `重試 ${n}/3`,
    loading_cluster:     '搜尋節點中…',

    // ── Leaderboard ──────────────────────────────────────────────────
    page_leaderboard:  '貢獻排行',
    section_board:     '節點貢獻排行',
    col_rank_label:    '排名',
    col_peer:          '節點',
    col_tasks:         '請求',
    col_in_tokens:     '輸入',
    col_out_tokens:    '輸出',
    col_total_tokens:  '總 Tokens',
    col_score:         '得分',
    col_score_hint:    '純顯卡等級(VRAM × 300 × 顯卡數量)，不受流量或上線時間影響',
    no_records:        '尚無資料',

    // ── History ──────────────────────────────────────────────────────
    page_history:      '刺查記錄',
    section_events:    '全局事件日誌',
    col_time:          '時間',
    col_event:         '事件',
    col_retries:       '重試',
    col_penalties:     '罰分',
    col_detail:        '詳情',
    no_events:         '尚無事件',
    records_count:     (n) => `${n} 筆`,
    evt_fail:          '失敗',
    evt_recover:       '復原',
    evt_offline:       '離線',
    evt_join:          '加入',

    // ── Chat ─────────────────────────────────────────────────────────
    page_chat:         '聊天室',
    chat_new:          '新增',
    chat_sessions:     '對話',
    chat_settings:     '模型設定',
    chat_clear:        '清除',
    chat_streaming:    '串流中…',
    chat_placeholder:  '輸入訊息… (Enter 發送, Shift+Enter 換行)',
    chat_empty_title:  '開始一段新對話',
    chat_storage_hint: 'Session 自動儲存於 localStorage',
    chat_attach_image: '附加圖片(需要對方節點的模型支援圖像辨識)',
    chat_remove_image: '移除圖片',
    cfg_endpoint:      'API Endpoint',
    cfg_model:         'Model',
    cfg_api_key:       'API Key（選填）',
    cfg_temperature:   'Temperature',
    cfg_max_tokens:    'Max Tokens',
    cfg_system:        'System Prompt（選填）',
    cfg_from_config:   (proxy, vllm, model) => `config.json → proxy :${proxy}  ·  vLLM :${vllm}  ·  model: ${model || '（未設定）'}`,
    cfg_reading:       '讀取 config.json 中…',
    cfg_reset:         '↺ 重設為 config.json',
    chat_user_me:      '我',

    // ── Logs ─────────────────────────────────────────────────────────
    page_logs:         '即時日誌',

    // ── Settings ─────────────────────────────────────────────────────
    page_settings:     '設定 & 備份',
  },
}

export function useI18n() {
  function t(key, ...args) {
    const val = messages[state.lang]?.[key] ?? messages['en']?.[key] ?? key
    return typeof val === 'function' ? val(...args) : val
  }

  function setLang(lang) {
    state.lang = lang
    localStorage.setItem(STORAGE_KEY, lang)
  }

  function toggleLang() {
    setLang(state.lang === 'en' ? 'zh' : 'en')
  }

  const langLabel = computed(() => state.lang === 'en' ? '中文' : 'EN')
  const isEn = computed(() => state.lang === 'en')

  return { t, lang: computed(() => state.lang), langLabel, isEn, setLang, toggleLang }
}

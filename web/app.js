// Configuration
const API_BASE_URL = ''; // Empty string means requests are relative (routed through Nginx proxy)
const HISTORY_KEY = 'shrinklink_history';

// Active chart instances (for proper destruction on reload)
let charts = {
  clicksTime: null,
  referrers: null,
  countries: null,
  browsers: null,
  os: null
};

// DOM Elements
const shortenForm = document.getElementById('shorten-form');
const longUrlInput = document.getElementById('long-url');
const customAliasInput = document.getElementById('custom-alias');
const expirySelect = document.getElementById('expiry-seconds');
const resultBox = document.getElementById('result-box');
const resultUrlInput = document.getElementById('result-url');
const copyBtn = document.getElementById('btn-copy');
const refreshBtn = document.getElementById('btn-refresh');
const historyList = document.getElementById('history-list');
const analyticsModal = document.getElementById('analytics-modal');
const closeModalBtn = document.getElementById('btn-close-modal');

// Modal Stats
const modalTitle = document.getElementById('modal-title');
const statTotalClicks = document.getElementById('stat-total-clicks');
const statTargetUrl = document.getElementById('stat-target-url');

// App Initialization
document.addEventListener('DOMContentLoaded', () => {
  renderHistory();

  // Shorten URL Form Submission
  shortenForm.addEventListener('submit', handleShorten);

  // Copy to Clipboard Action
  copyBtn.addEventListener('click', copyResultUrl);

  // Refresh History
  refreshBtn.addEventListener('click', renderHistory);

  // Close Analytics Modal
  closeModalBtn.addEventListener('click', () => {
    analyticsModal.classList.add('hidden');
    destroyAllCharts();
  });

  // Close Modal on Overlay Click
  analyticsModal.addEventListener('click', (e) => {
    if (e.target === analyticsModal) {
      analyticsModal.classList.add('hidden');
      destroyAllCharts();
    }
  });
});

// Shorten URL Handler
async function handleShorten(e) {
  e.preventDefault();
  const btnSubmit = document.getElementById('btn-submit');
  const btnSpan = btnSubmit.querySelector('span');
  
  const longURL = longUrlInput.value.trim();
  const alias = customAliasInput.value.trim();
  const expiryVal = expirySelect.value;
  
  const requestBody = { long_url: longURL };
  if (alias) requestBody.alias = alias;
  if (expiryVal) requestBody.expires_in = parseInt(expiryVal, 10);

  try {
    // UI Loading state
    btnSpan.textContent = 'Generating...';
    btnSubmit.disabled = true;

    const response = await fetch(`${API_BASE_URL}/api/shorten`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(requestBody)
    });

    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || 'Failed to shorten URL');
    }

    // Display Result
    resultUrlInput.value = data.short_url;
    resultBox.classList.remove('hidden');
    
    // Save to local history
    saveToHistory({
      shortCode: data.short_code,
      shortURL: data.short_url,
      longURL: data.long_url,
      createdAt: new Date().toISOString(),
      expiresAt: data.expires_at || null
    });

    // Reset Form
    shortenForm.reset();
    renderHistory();
  } catch (error) {
    alert(`Error: ${error.message}`);
  } finally {
    btnSpan.textContent = 'Generate Short URL';
    btnSubmit.disabled = false;
  }
}

// Copy Result to Clipboard
function copyResultUrl() {
  resultUrlInput.select();
  resultUrlInput.setSelectionRange(0, 99999);
  navigator.clipboard.writeText(resultUrlInput.value)
    .then(() => {
      const copySpan = copyBtn.querySelector('span');
      copySpan.textContent = 'Copied!';
      copyBtn.style.background = 'rgba(16, 185, 129, 0.2)';
      copyBtn.style.borderColor = 'var(--success)';
      setTimeout(() => {
        copySpan.textContent = 'Copy';
        copyBtn.style.background = '';
        copyBtn.style.borderColor = '';
      }, 2000);
    })
    .catch(err => console.error('Failed to copy: ', err));
}

// History Storage Logic
function getHistory() {
  const history = localStorage.getItem(HISTORY_KEY);
  return history ? JSON.parse(history) : [];
}

function saveToHistory(item) {
  const history = getHistory();
  // Avoid duplicates
  const index = history.findIndex(h => h.shortCode === item.shortCode);
  if (index > -1) {
    history[index] = item;
  } else {
    history.unshift(item);
  }
  localStorage.setItem(HISTORY_KEY, JSON.stringify(history));
}

function deleteFromHistory(shortCode) {
  let history = getHistory();
  history = history.filter(h => h.shortCode !== shortCode);
  localStorage.setItem(HISTORY_KEY, JSON.stringify(history));
  renderHistory();
}

// Render History Table
function renderHistory() {
  const history = getHistory();
  historyList.innerHTML = '';

  if (history.length === 0) {
    historyList.innerHTML = `
      <tr class="empty-state">
        <td colspan="5">No shortened URLs stored locally in session yet. Create one above!</td>
      </tr>
    `;
    return;
  }

  history.forEach(item => {
    const tr = document.createElement('tr');
    
    // Status/Expiry
    let statusBadge = '<span class="badge badge-active">Active</span>';
    if (item.expiresAt) {
      const isExpired = new Date(item.expiresAt) < new Date();
      if (isExpired) {
        statusBadge = '<span class="badge badge-expired">Expired</span>';
      } else {
        const remaining = Math.round((new Date(item.expiresAt) - new Date()) / 1000);
        let timeText = `${remaining}s`;
        if (remaining > 86400) timeText = `${Math.round(remaining/86400)}d`;
        else if (remaining > 3600) timeText = `${Math.round(remaining/3600)}h`;
        else if (remaining > 60) timeText = `${Math.round(remaining/60)}m`;
        statusBadge = `<span class="badge badge-active">Expires: ${timeText}</span>`;
      }
    }

    const createdDate = new Date(item.createdAt).toLocaleString(undefined, {
      month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
    });

    tr.innerHTML = `
      <td><a href="${item.shortURL}" target="_blank" class="short-link">${item.shortCode}</a></td>
      <td title="${item.longURL}"><a href="${item.longURL}" target="_blank" style="color: inherit; text-decoration: none;">${item.longURL}</a></td>
      <td>${createdDate}</td>
      <td>${statusBadge}</td>
      <td class="actions-cell">
        <button class="btn-icon btn-analytics" title="View Analytics" data-code="${item.shortCode}" data-long="${item.longURL}">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="20" x2="18" y2="10"></line>
            <line x1="12" y1="20" x2="12" y2="4"></line>
            <line x1="6" y1="20" x2="6" y2="14"></line>
          </svg>
        </button>
        <button class="btn-icon btn-delete" title="Remove locally" data-code="${item.shortCode}">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="3 6 5 6 21 6"></polyline>
            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
            <line x1="10" y1="11" x2="10" y2="17"></line>
            <line x1="14" y1="11" x2="14" y2="17"></line>
          </svg>
        </button>
      </td>
    `;

    // Hook events
    tr.querySelector('.btn-analytics').addEventListener('click', (e) => {
      const code = e.currentTarget.getAttribute('data-code');
      const longUrl = e.currentTarget.getAttribute('data-long');
      showAnalytics(code, longUrl);
    });

    tr.querySelector('.btn-delete').addEventListener('click', (e) => {
      const code = e.currentTarget.getAttribute('data-code');
      deleteFromHistory(code);
    });

    historyList.appendChild(tr);
  });
}

// Show Analytics Dashboard
async function showAnalytics(code, longUrl) {
  modalTitle.textContent = `Analytics for: /${code}`;
  statTargetUrl.textContent = longUrl;
  statTotalClicks.textContent = '...';
  
  analyticsModal.classList.remove('hidden');

  try {
    const response = await fetch(`${API_BASE_URL}/api/analytics/${code}`);
    const data = await response.json();

    if (!response.ok) {
      throw new Error(data.error || 'Failed to fetch analytics');
    }

    statTotalClicks.textContent = data.total_clicks;
    
    // Render Charts
    renderClicksTimeChart(data.clicks_over_time || []);
    renderPieChart('chart-referrers', 'referrers', data.referrers || []);
    renderPieChart('chart-countries', 'countries', data.countries || []);
    renderPieChart('chart-browsers', 'browsers', data.browsers || []);
    renderHorizontalBarChart('chart-os', 'os', data.os || []);

  } catch (error) {
    alert(`Error fetching analytics: ${error.message}`);
    analyticsModal.classList.add('hidden');
  }
}

// Clean up chart memory leaks
function destroyAllCharts() {
  Object.keys(charts).forEach(key => {
    if (charts[key]) {
      charts[key].destroy();
      charts[key] = null;
    }
  });
}

// Chart rendering functions
function renderClicksTimeChart(data) {
  if (charts.clicksTime) charts.clicksTime.destroy();

  // Fill in empty values if last 7 days lack details
  const labels = data.map(d => d.period);
  const values = data.map(d => d.clicks);

  const ctx = document.getElementById('chart-clicks-time').getContext('2d');
  
  // Create gradient
  const gradient = ctx.createLinearGradient(0, 0, 0, 200);
  gradient.addColorStop(0, 'rgba(168, 85, 247, 0.4)');
  gradient.addColorStop(1, 'rgba(168, 85, 247, 0)');

  charts.clicksTime = new Chart(ctx, {
    type: 'line',
    data: {
      labels: labels.length > 0 ? labels : ['No Activity'],
      datasets: [{
        label: 'Clicks',
        data: values.length > 0 ? values : [0],
        borderColor: '#a855f7',
        borderWidth: 2,
        backgroundColor: gradient,
        fill: true,
        tension: 0.4,
        pointBackgroundColor: '#a855f7'
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false }
      },
      scales: {
        x: {
          grid: { color: 'rgba(255, 255, 255, 0.05)' },
          ticks: { color: '#9ca3af', font: { size: 10 } }
        },
        y: {
          grid: { color: 'rgba(255, 255, 255, 0.05)' },
          ticks: { color: '#9ca3af', stepSize: 1 },
          beginAtZero: true
        }
      }
    }
  });
}

const colorPalette = [
  '#a855f7', // Purple
  '#3b82f6', // Blue
  '#10b981', // Emerald
  '#f59e0b', // Amber
  '#ec4899', // Pink
  '#6366f1', // Indigo
];

function renderPieChart(canvasId, chartKey, data) {
  if (charts[chartKey]) charts[chartKey].destroy();

  const labels = data.map(d => d.name);
  const values = data.map(d => d.count);

  const ctx = document.getElementById(canvasId).getContext('2d');

  charts[chartKey] = new Chart(ctx, {
    type: 'doughnut',
    data: {
      labels: labels.length > 0 ? labels : ['No Data'],
      datasets: [{
        data: values.length > 0 ? values : [1],
        backgroundColor: values.length > 0 ? colorPalette : ['rgba(255, 255, 255, 0.05)'],
        borderWidth: 1,
        borderColor: 'rgba(13, 12, 21, 0.8)'
      }]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: {
          position: 'right',
          labels: {
            color: '#9ca3af',
            font: { size: 11 },
            boxWidth: 12
          }
        }
      },
      cutout: '65%'
    }
  });
}

function renderHorizontalBarChart(canvasId, chartKey, data) {
  if (charts[chartKey]) charts[chartKey].destroy();

  const labels = data.map(d => d.name);
  const values = data.map(d => d.count);

  const ctx = document.getElementById(canvasId).getContext('2d');

  charts[chartKey] = new Chart(ctx, {
    type: 'bar',
    data: {
      labels: labels.length > 0 ? labels : ['No Data'],
      datasets: [{
        data: values.length > 0 ? values : [0],
        backgroundColor: colorPalette[1], // Indigo / Blue
        borderRadius: 6
      }]
    },
    options: {
      indexAxis: 'y',
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false }
      },
      scales: {
        x: {
          grid: { color: 'rgba(255, 255, 255, 0.05)' },
          ticks: { color: '#9ca3af', stepSize: 1 }
        },
        y: {
          grid: { display: false },
          ticks: { color: '#9ca3af' }
        }
      }
    }
  });
}

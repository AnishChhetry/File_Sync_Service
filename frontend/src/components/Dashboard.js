import React from 'react';
import './Dashboard.css';

function Dashboard({ status, onPause, onResume, onManualSync, syncing }) {
  const getStatusColor = () => {
    if (status.isPaused) return '#f59e0b';
    switch (status.status) {
      case 'running':
        return '#10b981';
      case 'error':
        return '#ef4444';
      case 'connecting':
        return '#f59e0b';
      default:
        return '#6b7280';
    }
  };

  const getStatusText = () => {
    if (status.isPaused) return 'Paused';
    return status.status;
  };

  return (
    <div className="dashboard">
      <div className="stat-card">
        <div className="stat-icon" style={{ backgroundColor: '#3b82f6' }}>📁</div>
        <div className="stat-content">
          <div className="stat-label">Local Files</div>
          <div className="stat-value">{status.localFiles}</div>
        </div>
      </div>

      <div className="stat-card">
        <div className="stat-icon" style={{ backgroundColor: '#8b5cf6' }}>☁️</div>
        <div className="stat-content">
          <div className="stat-label">Remote Files</div>
          <div className="stat-value">{status.remoteFiles}</div>
        </div>
      </div>

      <div className="stat-card">
        <div className="stat-icon" style={{ backgroundColor: getStatusColor() }}>⚡</div>
        <div className="stat-content">
          <div className="stat-label">Sync Status</div>
          <div className="stat-value" style={{ fontSize: '16px', textTransform: 'capitalize' }}>
            {getStatusText()}
          </div>
        </div>
      </div>

      <div className="stat-card controls-card">
        <div className="controls">
          <button 
            className={`control-btn ${status.isPaused ? 'autosync-off-btn' : 'autosync-on-btn'}`}
            onClick={status.isPaused ? onResume : onPause}
          >
            {status.isPaused ? '🔴 Auto-Sync: OFF' : '🟢 Auto-Sync: ON'}
          </button>
          <button 
            className="control-btn sync-btn"
            onClick={onManualSync}
            disabled={syncing || status.isPaused}
          >
            {syncing ? '🔄 Syncing...' : '🔄 Sync Now'}
          </button>
        </div>
      </div>
    </div>
  );
}

export default Dashboard;

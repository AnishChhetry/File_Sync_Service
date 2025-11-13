import React, { useMemo, useState } from 'react';
import './ActivityLog.css';

function ActivityLog({ activities }) {
  const [page, setPage] = useState(0);
  const pageSize = 25;

  const totalActivities = activities || [];
  const totalPages = Math.ceil(totalActivities.length / pageSize);
  const currentPage = Math.min(page, Math.max(totalPages - 1, 0));

  const displayedActivities = useMemo(() => {
    const start = currentPage * pageSize;
    return totalActivities.slice(start, start + pageSize);
  }, [totalActivities, currentPage]);

  const hasMore = totalPages > 1;

  const handlePrev = () => {
    setPage((prev) => Math.max(prev - 1, 0));
  };

  const handleNext = () => {
    setPage((prev) => Math.min(prev + 1, Math.max(totalPages - 1, 0)));
  };

  const getEventIcon = (type) => {
    const icons = {
      'create': '✨',
      'update': '✏️',
      'delete': '🗑️',
      'sync': '🔄',
      'pause': '⏸️',
      'resume': '▶️',
      'error': '❌'
    };
    return icons[type] || '📌';
  };

  const formatTime = (timestamp) => {
    const date = new Date(timestamp);
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  };

  const getDirectionLabel = (direction) => {
    if (!direction) return null;

    const map = {
      local_to_remote: 'Local → Remote',
      remote_to_local: 'Remote → Local',
      both: 'Local ↔ Remote',
      '>': 'Local → Remote',
      '<': 'Remote → Local',
    };

    return map[direction] || direction;
  };

  if (!totalActivities || totalActivities.length === 0) {
    return (
      <div className="empty-state">
        <div className="empty-icon">📋</div>
        <p>No activity yet</p>
        <p className="empty-hint">Sync events will appear here in real-time</p>
      </div>
    );
  }

  return (
    <div className="activity-log">
      {displayedActivities.map((activity) => (
        <div key={activity.id} className="activity-item">
          <div className="activity-icon">{getEventIcon(activity.type)}</div>
          <div className="activity-content">
            <div className="activity-message">
              {activity.message || `${activity.type} ${activity.filePath || ''}`}
            </div>
            <div className="activity-meta">
              {activity.filePath && (
                <span className="activity-file">{activity.filePath}</span>
              )}
              {getDirectionLabel(activity.direction) && (
                <span className="activity-direction">{getDirectionLabel(activity.direction)}</span>
              )}
              <span className="activity-time">{formatTime(activity.timestamp)}</span>
            </div>
          </div>
        </div>
      ))}
      {hasMore && (
        <div className="activity-pagination" role="status">
          <button
            type="button"
            className="activity-page-btn"
            onClick={handlePrev}
            disabled={currentPage === 0}
          >
            ← Newer
          </button>
          <span className="activity-page-info">
            Page {currentPage + 1} of {Math.max(totalPages, 1)}
          </span>
          <button
            type="button"
            className="activity-page-btn"
            onClick={handleNext}
            disabled={currentPage >= totalPages - 1}
          >
            Older →
          </button>
        </div>
      )}
    </div>
  );
}

export default ActivityLog;

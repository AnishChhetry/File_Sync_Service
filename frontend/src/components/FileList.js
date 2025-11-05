import React from 'react';
import './FileList.css';

function FileList({ files }) {
  const getLocationBadge = (location) => {
    const badges = {
      'local': { text: 'Local', color: '#3b82f6' },
      'remote': { text: 'Remote', color: '#8b5cf6' },
      'both': { text: 'Synced', color: '#10b981' }
    };
    
    const badge = badges[location] || badges['both'];
    
    return (
      <span className="location-badge" style={{ backgroundColor: badge.color }}>
        {badge.text}
      </span>
    );
  };

  if (!files || files.length === 0) {
    return (
      <div className="empty-state">
        <div className="empty-icon">📭</div>
        <p>No files synced yet</p>
        <p className="empty-hint">Add files to local_data or remote_data folders</p>
      </div>
    );
  }

  return (
    <div className="file-list">
      <div className="file-list-header">
        <div className="file-col-name">File Path</div>
        <div className="file-col-location">Location</div>
        <div className="file-col-modified">Last Modified</div>
      </div>
      <div className="file-list-body">
        {files.map((file, index) => (
          <div key={index} className="file-item">
            <div className="file-col-name">
              <span className="file-icon">📄</span>
              <span className="file-path" title={file.relativePath}>
                {file.relativePath}
              </span>
            </div>
            <div className="file-col-location">
              {getLocationBadge(file.location)}
            </div>
            <div className="file-col-modified">
              {file.modTime}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default FileList;

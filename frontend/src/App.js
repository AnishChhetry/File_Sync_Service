import React, { useState, useEffect, useRef, useCallback } from 'react';
import './App.css';
import Dashboard from './components/Dashboard';
import FileList from './components/FileList';
import ActivityLog from './components/ActivityLog';

const API_URL = (process.env.REACT_APP_API_URL || 'http://localhost:8080').replace(/\/$/, '');
const WS_URL = (process.env.REACT_APP_WS_URL || '').replace(/\/$/, '') || `${API_URL.replace(/^http/, 'ws')}/ws`;

const normalizeActivity = (activity) => {
  const eventTime = activity?.timestamp ? new Date(activity.timestamp) : new Date();
  const timeMs = Number.isNaN(eventTime.getTime()) ? Date.now() : eventTime.getTime();

  return {
    ...activity,
    timestamp: new Date(timeMs).toISOString(),
    timestampMs: timeMs,
    id:
      activity?.id ||
      `${timeMs}-${activity?.type || 'event'}-${activity?.filePath || 'system'}-${Math.random()
        .toString(36)
        .slice(2, 8)}`,
  };
};

function App() {
  const [status, setStatus] = useState({
    status: 'connecting',
    localFiles: 0,
    remoteFiles: 0,
    isRunning: false
  });
  const [files, setFiles] = useState([]);
  const [activities, setActivities] = useState([]);
  const [wsConnected, setWsConnected] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [statusError, setStatusError] = useState('');
  const [filesError, setFilesError] = useState('');
  const [connectionNotice, setConnectionNotice] = useState('');
  const wsRef = useRef(null);
  const reconnectTimeoutRef = useRef(null);
  const shouldReconnectRef = useRef(true);

  const pushActivity = useCallback((activity) => {
    setActivities((prev) => {
      const normalized = normalizeActivity(activity);

      const normalizedPrev = prev.map((item) => (item.timestampMs ? item : normalizeActivity(item)));

      const merged = [normalized, ...normalizedPrev];
      const seen = new Map();

      for (const item of merged) {
        if (!seen.has(item.id)) {
          seen.set(item.id, item);
        }
      }

      return Array.from(seen.values())
        .sort((a, b) => (b.timestampMs || 0) - (a.timestampMs || 0))
        .slice(0, 50);
    });
  }, []);

  const fetchStatus = useCallback(async () => {
    try {
      setStatusError('');
      const response = await fetch(`${API_URL}/api/status`);
      if (!response.ok) {
        throw new Error(`Request failed with status ${response.status}`);
      }
      const data = await response.json();
      setStatus(data);
    } catch (error) {
      console.error('Error fetching status:', error);
      setStatus(prev => ({ ...prev, status: 'error' }));
      setStatusError('Unable to fetch sync status. Displaying last known information.');
    }
  }, []);

  const fetchFiles = useCallback(async () => {
    try {
      setFilesError('');
      const response = await fetch(`${API_URL}/api/files`);
      if (!response.ok) {
        throw new Error(`Request failed with status ${response.status}`);
      }
      const data = await response.json();
      setFiles(data || []);
    } catch (error) {
      console.error('Error fetching files:', error);
      setFilesError('Unable to fetch file list. Displaying last known results.');
    }
  }, []);

  const connectWebSocket = useCallback(() => {
    if (!WS_URL) {
      return;
    }

    try {
      const ws = new WebSocket(WS_URL);
      wsRef.current = ws;

      ws.onopen = () => {
        console.log('WebSocket connected');
        setWsConnected(true);
        setConnectionNotice('');
        if (reconnectTimeoutRef.current) {
          clearTimeout(reconnectTimeoutRef.current);
          reconnectTimeoutRef.current = null;
        }
      };

      ws.onmessage = (event) => {
        const syncEvent = JSON.parse(event.data);
        console.log('Received sync event:', syncEvent);

        pushActivity(syncEvent);
        fetchFiles();
        fetchStatus();
      };

      ws.onerror = (error) => {
        console.error('WebSocket error:', error);
        setConnectionNotice('Real-time connection error. Attempting to reconnect...');
        ws.close();
      };

      ws.onclose = () => {
        const isLatestSocket = wsRef.current === ws;
        if (isLatestSocket) {
          setWsConnected(false);
          wsRef.current = null;
        }

        if (!shouldReconnectRef.current || !isLatestSocket) {
          return;
        }

        setConnectionNotice('Real-time updates unavailable. Attempting to reconnect...');
        if (reconnectTimeoutRef.current) {
          clearTimeout(reconnectTimeoutRef.current);
        }
        reconnectTimeoutRef.current = setTimeout(connectWebSocket, 3000);
      };
    } catch (error) {
      console.error('Unable to establish WebSocket connection:', error);
      setConnectionNotice('Unable to establish real-time connection.');
    }
  }, [fetchFiles, fetchStatus, pushActivity]);

  useEffect(() => {
    // Fetch initial status
    fetchStatus();
    fetchFiles();

    // Set up polling for status
    const statusInterval = setInterval(fetchStatus, 5000);

    shouldReconnectRef.current = true;

    if (WS_URL) {
      connectWebSocket();
    } else {
      setConnectionNotice('Real-time updates unavailable: WebSocket URL not configured.');
    }

    return () => {
      shouldReconnectRef.current = false;
      clearInterval(statusInterval);
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
        reconnectTimeoutRef.current = null;
      }
      if (wsRef.current) {
        try {
          wsRef.current.close();
        } catch (error) {
          console.error('Error closing WebSocket:', error);
        } finally {
          wsRef.current = null;
        }
      }
    };
  }, [fetchStatus, fetchFiles, connectWebSocket]);

  const handleRetryStatus = useCallback(() => {
    fetchStatus();
  }, [fetchStatus]);

  const handleRetryFiles = useCallback(() => {
    fetchFiles();
  }, [fetchFiles]);

  const handleRetryConnection = useCallback(() => {
    if (!WS_URL) {
      setConnectionNotice('Real-time updates unavailable: WebSocket URL not configured.');
      return;
    }

    setConnectionNotice('Attempting to reconnect...');

    if (reconnectTimeoutRef.current) {
      clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    if (wsRef.current) {
      try {
        const existing = wsRef.current;
        wsRef.current = null;
        existing.close();
      } catch (error) {
        console.error('Error closing existing WebSocket:', error);
      }
    }

    shouldReconnectRef.current = true;
    connectWebSocket();
  }, [connectWebSocket]);

  const handlePause = async () => {
    try {
      const response = await fetch(`${API_URL}/api/pause`, { method: 'POST' });
      const data = await response.json();
      console.log('Paused:', data);
      fetchStatus();
      pushActivity({
        type: 'pause',
        filePath: 'system',
        message: 'Auto-sync paused',
        direction: '',
      });
    } catch (error) {
      console.error('Error pausing sync:', error);
    }
  };

  const handleResume = async () => {
    try {
      const response = await fetch(`${API_URL}/api/resume`, { method: 'POST' });
      const data = await response.json();
      console.log('Resumed:', data);
      fetchStatus();
      pushActivity({
        type: 'resume',
        filePath: 'system',
        message: 'Auto-sync resumed',
        direction: '',
      });
    } catch (error) {
      console.error('Error resuming sync:', error);
    }
  };

  const handleManualSync = async () => {
    if (syncing) return;
    
    setSyncing(true);
    try {
      const response = await fetch(`${API_URL}/api/sync`, { method: 'POST' });
      const data = await response.json();
      console.log('Manual sync:', data);
      
      if (data.success) {
        pushActivity({
          type: 'sync',
          filePath: 'manual',
          message: 'Manual sync completed',
          direction: 'both',
        });
        
        // Refresh data after sync
        setTimeout(() => {
          fetchStatus();
          fetchFiles();
        }, 500);
      }
    } catch (error) {
      console.error('Error during manual sync:', error);
      pushActivity({
        type: 'error',
        filePath: 'manual',
        message: 'Manual sync failed: ' + error.message,
        direction: '',
      });
    } finally {
      setSyncing(false);
    }
  };

  return (
    <div className="App">
      <header className="app-header">
        <h1>📁 File Sync Service</h1>
        <div className="connection-status">
          <span className={`status-indicator ${wsConnected ? 'connected' : 'disconnected'}`}></span>
          {wsConnected ? 'Live' : 'Disconnected'}
        </div>
      </header>
      
      <main className="app-main">
        {connectionNotice && (
          <div className="alert alert-warning" role="status">
            <span aria-hidden="true">🔌</span>
            <span className="alert-message">{connectionNotice}</span>
            {WS_URL && (
              <div className="alert-actions">
                <button type="button" className="alert-action" onClick={handleRetryConnection}>
                  Retry
                </button>
              </div>
            )}
          </div>
        )}
        {statusError && (
          <div className="alert alert-error" role="alert">
            <span aria-hidden="true">⚠️</span>
            <span className="alert-message">{statusError}</span>
            <div className="alert-actions">
              <button type="button" className="alert-action" onClick={handleRetryStatus}>
                Retry
              </button>
            </div>
          </div>
        )}
        <Dashboard 
          status={status} 
          onPause={handlePause}
          onResume={handleResume}
          onManualSync={handleManualSync}
          syncing={syncing}
        />
        
        <div className="content-grid">
          <div className="panel">
            <h2>📂 Synced Files</h2>
            {filesError && (
              <div className="alert alert-error panel-alert" role="alert">
                <span aria-hidden="true">⚠️</span>
                <span className="alert-message">{filesError}</span>
                <div className="alert-actions">
                  <button type="button" className="alert-action" onClick={handleRetryFiles}>
                    Retry
                  </button>
                </div>
              </div>
            )}
            <FileList files={files} />
          </div>
          
          <div className="panel">
            <h2>📊 Activity Log</h2>
            <ActivityLog activities={activities} />
          </div>
        </div>
      </main>
    </div>
  );
}

export default App;

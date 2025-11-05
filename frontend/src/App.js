import React, { useState, useEffect, useRef } from 'react';
import './App.css';
import Dashboard from './components/Dashboard';
import FileList from './components/FileList';
import ActivityLog from './components/ActivityLog';

const API_URL = 'http://localhost:8080';
const WS_URL = 'ws://localhost:8080/ws';

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
  const wsRef = useRef(null);
  const reconnectTimeoutRef = useRef(null);

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

  const pushActivity = (activity) => {
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
  };

  useEffect(() => {
    // Fetch initial status
    fetchStatus();
    fetchFiles();

    // Set up polling for status
    const statusInterval = setInterval(fetchStatus, 5000);

    let shouldReconnect = true;

    const connectWebSocket = () => {
      const ws = new WebSocket(WS_URL);
      wsRef.current = ws;

      ws.onopen = () => {
        console.log('WebSocket connected');
        setWsConnected(true);
        if (reconnectTimeoutRef.current) {
          clearTimeout(reconnectTimeoutRef.current);
          reconnectTimeoutRef.current = null;
        }
      };

      ws.onmessage = (event) => {
        const syncEvent = JSON.parse(event.data);
        console.log('Received sync event:', syncEvent);
        
        // Add to activity log
        pushActivity(syncEvent);

        // Refresh file list
        fetchFiles();
        fetchStatus();
      };

      ws.onerror = (error) => {
        console.error('WebSocket error:', error);
        ws.close();
      };

      ws.onclose = () => {
        console.log('WebSocket disconnected');
        setWsConnected(false);
        wsRef.current = null;
        if (shouldReconnect) {
          if (reconnectTimeoutRef.current) {
            clearTimeout(reconnectTimeoutRef.current);
          }
          reconnectTimeoutRef.current = setTimeout(connectWebSocket, 3000);
        }
      };
    };

    connectWebSocket();

    return () => {
      shouldReconnect = false;
      clearInterval(statusInterval);
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
        reconnectTimeoutRef.current = null;
      }
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, []);

  const fetchStatus = async () => {
    try {
      const response = await fetch(`${API_URL}/api/status`);
      const data = await response.json();
      setStatus(data);
    } catch (error) {
      console.error('Error fetching status:', error);
      setStatus(prev => ({ ...prev, status: 'error' }));
    }
  };

  const fetchFiles = async () => {
    try {
      const response = await fetch(`${API_URL}/api/files`);
      const data = await response.json();
      setFiles(data || []);
    } catch (error) {
      console.error('Error fetching files:', error);
    }
  };

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

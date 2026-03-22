import React, { useState, useEffect } from 'react';
import { StyleSheet, Text, View, TextInput, TouchableOpacity, FlatList, ActivityIndicator, Platform, Alert } from 'react-native';
import * as SecureStore from 'expo-secure-store';

// Android emulator uses 10.0.2.2 to access host localhost, iOS simulator uses localhost
const API_BASE_URL = Platform.OS === 'android' ? 'http://10.0.2.2:8000' : 'http://localhost:8000';

export default function App() {
  const [token, setToken] = useState(null);
  const [isLoading, setIsLoading] = useState(true);

  // Login Form State
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [loginError, setLoginError] = useState('');

  // Dashboard State
  const [leads, setLeads] = useState([]);
  const [fetchingLeads, setFetchingLeads] = useState(false);

  useEffect(() => {
    // Check if token exists on startup
    const checkToken = async () => {
      try {
        const storedToken = await SecureStore.getItemAsync('userToken');
        if (storedToken) {
          setToken(storedToken);
        }
      } catch (e) {
        console.warn('Silent read error:', e);
      } finally {
        setIsLoading(false);
      }
    };
    checkToken();
  }, []);

  useEffect(() => {
    if (token) {
      fetchLeads();
    }
  }, [token]);

  const handleLogin = async () => {
    if (!email || !password) {
      setLoginError('Email and Password are required');
      return;
    }
    setLoginError('');
    setIsLoading(true);

    try {
      const formData = new URLSearchParams();
      formData.append('username', email);
      formData.append('password', password);

      const response = await fetch(`${API_BASE_URL}/api/auth/login`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/x-www-form-urlencoded',
        },
        body: formData.toString()
      });

      const data = await response.json();
      if (response.ok && data.access_token) {
        await SecureStore.setItemAsync('userToken', data.access_token);
        setToken(data.access_token);
      } else {
        setLoginError(data.detail || 'Login failed');
      }
    } catch (error) {
      setLoginError('Network Error. Is the backend running?');
    } finally {
      setIsLoading(false);
    }
  };

  const handleLogout = async () => {
    await SecureStore.deleteItemAsync('userToken');
    setToken(null);
    setLeads([]);
  };

  const fetchLeads = async () => {
    setFetchingLeads(true);
    try {
      const response = await fetch(`${API_BASE_URL}/api/mobile/leads`, {
        headers: {
          'Authorization': `Bearer ${token}`
        }
      });
      if (response.status === 401) {
        // Token expired/invalid
        handleLogout();
        Alert.alert("Session Expired", "Please log in again.");
      } else {
        const data = await response.json();
        setLeads(data);
      }
    } catch (error) {
      Alert.alert("Error", "Could not fetch leads.");
    } finally {
      setFetchingLeads(false);
    }
  };

  if (isLoading && !token && !loginError) {
    return (
      <View style={styles.center}>
        <ActivityIndicator size="large" color="#0000ff" />
      </View>
    );
  }

  // --- LOGIN SCREEN ---
  if (!token) {
    return (
      <View style={styles.container}>
        <Text style={styles.title}>Globussoft API Login</Text>
        {loginError ? <Text style={styles.errorText}>{loginError}</Text> : null}
        
        <TextInput
          style={styles.input}
          placeholder="Email address"
          value={email}
          onChangeText={setEmail}
          autoCapitalize="none"
          keyboardType="email-address"
        />
        <TextInput
          style={styles.input}
          placeholder="Password"
          value={password}
          onChangeText={setPassword}
          secureTextEntry
        />
        
        <TouchableOpacity style={styles.button} onPress={handleLogin} disabled={isLoading}>
          {isLoading ? <ActivityIndicator color="#fff" /> : <Text style={styles.buttonText}>Secure Login</Text>}
        </TouchableOpacity>
      </View>
    );
  }

  // --- DASHBOARD SCREEN ---
  return (
    <View style={styles.dashboardContainer}>
      <View style={styles.header}>
        <Text style={styles.headerTitle}>CRM Leads</Text>
        <TouchableOpacity onPress={handleLogout} style={styles.logoutButton}>
          <Text style={styles.logoutText}>Logout</Text>
        </TouchableOpacity>
      </View>

      {fetchingLeads ? (
        <ActivityIndicator size="large" color="#0000ff" style={{ marginTop: 20 }} />
      ) : (
        <FlatList
          data={leads}
          keyExtractor={(item) => (item.id ? item.id.toString() : Math.random().toString())}
          renderItem={({ item }) => (
            <View style={styles.leadCard}>
              <Text style={styles.leadName}>{item.first_name} {item.last_name}</Text>
              <Text style={styles.leadPhone}>{item.phone}</Text>
              <Text style={styles.leadStatus}>Status: <Text style={{ fontWeight: 'bold' }}>{item.status}</Text></Text>
            </View>
          )}
          contentContainerStyle={{ padding: 20 }}
          ListEmptyComponent={<Text style={styles.emptyText}>No leads found in CRM.</Text>}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#f5f7fa',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 20,
  },
  center: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
  },
  title: {
    fontSize: 24,
    fontWeight: 'bold',
    marginBottom: 30,
    color: '#333',
  },
  input: {
    width: '100%',
    height: 50,
    backgroundColor: '#fff',
    borderRadius: 8,
    paddingHorizontal: 15,
    marginBottom: 15,
    borderWidth: 1,
    borderColor: '#ddd',
  },
  button: {
    width: '100%',
    height: 50,
    backgroundColor: '#007AFF',
    borderRadius: 8,
    alignItems: 'center',
    justifyContent: 'center',
    marginTop: 10,
  },
  buttonText: {
    color: '#fff',
    fontSize: 16,
    fontWeight: '600',
  },
  errorText: {
    color: '#ff3b30',
    marginBottom: 15,
    textAlign: 'center',
  },
  dashboardContainer: {
    flex: 1,
    backgroundColor: '#f5f7fa',
    paddingTop: 50,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingHorizontal: 20,
    paddingBottom: 15,
    borderBottomWidth: 1,
    borderBottomColor: '#ddd',
    backgroundColor: '#fff',
  },
  headerTitle: {
    fontSize: 20,
    fontWeight: 'bold',
  },
  logoutButton: {
    padding: 8,
  },
  logoutText: {
    color: '#ff3b30',
    fontWeight: '600',
  },
  leadCard: {
    backgroundColor: '#fff',
    padding: 15,
    borderRadius: 10,
    marginBottom: 15,
    shadowColor: '#000',
    shadowOpacity: 0.1,
    shadowOffset: { width: 0, height: 2 },
    shadowRadius: 4,
    elevation: 2,
  },
  leadName: {
    fontSize: 18,
    fontWeight: 'bold',
    color: '#222',
    marginBottom: 5,
  },
  leadPhone: {
    fontSize: 14,
    color: '#666',
    marginBottom: 5,
  },
  leadStatus: {
    fontSize: 14,
    color: '#444',
  },
  emptyText: {
    textAlign: 'center',
    marginTop: 50,
    color: '#888',
    fontSize: 16,
  }
});

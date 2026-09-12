import { initializeApp } from "https://www.gstatic.com/firebasejs/10.9.0/firebase-app.js";
import { getAuth, signInWithPopup, GoogleAuthProvider, signOut, onAuthStateChanged, getIdToken } from "https://www.gstatic.com/firebasejs/10.9.0/firebase-auth.js";

const firebaseConfig = {
  apiKey: "AIzaSyBBU1olUhNGzQsCI_4jRSNlP4WuSaQ7rho",
  authDomain: "shrink-link-5418e.firebaseapp.com",
  projectId: "shrink-link-5418e",
  storageBucket: "shrink-link-5418e.firebasestorage.app",
  messagingSenderId: "582289359138",
  appId: "1:582289359138:web:2766115763c5d99b28257b",
  measurementId: "G-KQFX70FT4D"
};

const app = initializeApp(firebaseConfig);
export const auth = getAuth(app);
const provider = new GoogleAuthProvider();

export let currentUserToken = null;

export function initAuth(onAuthStateChangeCallback) {
  onAuthStateChanged(auth, async (user) => {
    if (user) {
      currentUserToken = await getIdToken(user);
    } else {
      currentUserToken = null;
    }
    onAuthStateChangeCallback(user);
  });
}

export async function login() {
  try {
    await signInWithPopup(auth, provider);
  } catch (error) {
    console.error("Login failed", error);
    alert("Login failed: " + error.message);
  }
}

export async function logout() {
  try {
    await signOut(auth);
  } catch (error) {
    console.error("Logout failed", error);
  }
}

// Intercept fetch to add token
export async function fetchWithAuth(url, options = {}) {
  if (currentUserToken) {
    if (!options.headers) {
      options.headers = {};
    }
    options.headers['Authorization'] = `Bearer ${currentUserToken}`;
  }
  return fetch(url, options);
}

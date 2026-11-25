import { HttpClient } from '@angular/common/http';
import { Component, inject, signal } from '@angular/core';
import { RouterLink, RouterOutlet } from '@angular/router';

interface ApiHealth {
  status: string;
  service: string;
}

@Component({
  selector: 'app-root',
  imports: [RouterLink, RouterOutlet],
  templateUrl: './app.html',
  styleUrl: './app.css'
})
export class App {
  private readonly http = inject(HttpClient);
  protected readonly apiStatus = signal<'loading' | 'online' | 'offline'>('loading');

  constructor() {
    this.http.get<ApiHealth>('/api/health').subscribe({
      next: (health) => {
        this.apiStatus.set(health.status === 'ok' ? 'online' : 'offline');
      },
      error: () => {
        this.apiStatus.set('offline');
      }
    });
  }
}
